#define _GNU_SOURCE

#include <errno.h>
#include <fnmatch.h>
#include <numaif.h>
#include <stdarg.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <unistd.h>

#define DEFAULT_BLOCK_SIZE (2UL * 1024 * 1024)
#define PROGRESS_STEP (1UL * 1024 * 1024 * 1024)

void printfe(const char *format, ...) {
  va_list args;

  fprintf(stderr, "movepages: ");
  va_start(args, format);
  vfprintf(stderr, format, args);
  va_end(args);

  exit(EXIT_FAILURE);
}

void perrorfe(const char *format, ...) {
  va_list args;

  fprintf(stderr, "movepages: ");
  va_start(args, format);
  vfprintf(stderr, format, args);
  va_end(args);
  perror(NULL);
  exit(EXIT_FAILURE);
}

static long long parse_size(const char *errctx, const char *s) {
  char *end;
  errno = 0;
  unsigned long long val = strtoull(s, &end, 10);
  if (errno != 0 || end == s)
    printfe("parsing %s failed: invalid syntax: %s\n", errctx, s);

  if (*end != '\0') {
    switch (*end) {
    case 'k':
    case 'K':
      val *= 1024ULL;
      break;
    case 'm':
    case 'M':
      val *= 1024ULL * 1024ULL;
      break;
    case 'g':
    case 'G':
      val *= 1024ULL * 1024ULL * 1024ULL;
      break;
    default:
      printfe("parsing %s failed: unknown suffix '%c' in '%s'\n", errctx, *end,
              s);
    }
    if (*(end + 1) != '\0')
      printfe("parsing %s failed: trailing characters after suffix in '%s'\n",
              errctx, s);
  }

  long page_size = sysconf(_SC_PAGESIZE);
  if (page_size <= 0)
    page_size = 4096;

  long long rounded = ((long long)val + page_size - 1) / (long long)page_size *
                      (long long)page_size;
  if (rounded == 0)
    printfe("parsing %s failed: block size must be greater than zero\n",
            errctx);
  return rounded;
}

static long long *parse_ints(const char *errctx, const char *s,
                             long long parse_tok(const char *, const char *),
                             int *count) {
  char *copy = strdup(s);
  if (!copy)
    perrorfe("parsing %s failed: strdup", errctx);

  int cap = 8;
  int n = 0;
  long long *ints = malloc(cap * sizeof(long long));
  if (!ints)
    perrorfe("parsing %s failed: malloc", errctx);

  char *tok = strtok(copy, ",");

  while (tok) {
    if (n == cap) {
      cap *= 2;
      ints = realloc(ints, cap * sizeof(long long));
      if (!ints)
        perrorfe("parsing %s failed: realloc", errctx);
    }

    if (parse_tok)
      ints[n++] = parse_tok(errctx, tok);
    else
      ints[n++] = atoll(tok);
    tok = strtok(NULL, ",");
  }

  free(copy);

  *count = n;
  return ints;
}

static void print_usage(const char *prog) {
  fprintf(stderr,
          "Usage:\n"
          "  %s -b blocksize[,...] -i <interleave-node,...> -p <pid,...> [-f "
          "<glob> [-f <glob> ...]]\n\n"
          "Example:\n"
          "  %s -b 8M -i 2,3,0,2,3,1 -p 12345,23456 -f '*.gguf'\n",
          prog, prog);
}

static int matches_any_glob(char **globs, int nglobs, const char *path) {
  for (int i = 0; i < nglobs; i++) {
    if (fnmatch(globs[i], path, 0) == 0)
      return 1;
  }
  return 0;
}

int main(int argc, char **argv) {
  long long *nodes = NULL;
  int nnodes = 0;
  long long *pids = NULL;
  int npids = 0;
  char **globs = NULL;
  int nglobs = 0;
  int globs_cap = 0;
  long long *block_sizes = NULL;
  int nblock_sizes = 0;
  int verbose = 0;

  int opt;
  while ((opt = getopt(argc, argv, "b:i:p:f:v")) != -1) {
    switch (opt) {
    case 'b':
      free(block_sizes);
      block_sizes = parse_ints("-b blocksize[,blocksize...]", optarg,
                               parse_size, &nblock_sizes);
      break;
    case 'i':
      free(nodes);
      nodes = parse_ints("-i node,[node...]>", optarg, NULL, &nnodes);
      break;
    case 'p':
      free(pids);
      pids = parse_ints("-p pid[,pid...]", optarg, NULL, &npids);
      break;
    case 'f':
      if (nglobs == globs_cap) {
        globs_cap = globs_cap ? globs_cap * 2 : 8;
        globs = realloc(globs, globs_cap * sizeof(char *));
        if (!globs)
          perrorfe("parsing globs failed: realloc");
      }
      globs[nglobs] = strdup(optarg);
      if (!globs[nglobs])
        perrorfe("parsing globs failed: strdup");
      nglobs++;
      break;
    case 'v':
      verbose++;
      break;
    default:
      print_usage(argv[0]);
      return 1;
    }
  }

  if (!nodes || nnodes == 0) {
    print_usage(argv[0]);
    printfe("missing -i <node-list> argument\n");
  }
  if (!pids || npids == 0) {
    print_usage(argv[0]);
    printfe("missing -p <pid-list> argument\n");
  }
  if (!block_sizes || nblock_sizes == 0) {
    nblock_sizes = 1;
    block_sizes = malloc(nblock_sizes * sizeof(long long));
    block_sizes[0] = DEFAULT_BLOCK_SIZE;
  }

  long pagesize = sysconf(_SC_PAGESIZE);

  long long max_block_size = 0;
  for (int bsi = 0; bsi < nblock_sizes; bsi++) {
    if (block_sizes[bsi] > max_block_size) {
      max_block_size = block_sizes[bsi];
    }
  }

  long max_pages = max_block_size / pagesize;
  void **pages = malloc(max_pages * sizeof(void *));
  int *dest = malloc(max_pages * sizeof(int));
  int *status = malloc(max_pages * sizeof(int));
  if (!pages || !dest || !status) {
    perrorfe("malloc for arrays of %ld pages failed", max_pages);
    return 1;
  }

  if (verbose > 2) {
    printf("block sizes: ");
    for (int bsi = 0; bsi < nblock_sizes; bsi++) {
      printf("%lld ", block_sizes[bsi]);
    }
    printf(" (max %lld)\n", max_block_size);
    printf("page size: %ld\n", pagesize);
    printf("page array lengths: %ld\n", max_pages);
  }

  for (int pi = 0; pi < npids; pi++) {
    pid_t pid = (pid_t)pids[pi];

    char mapsfile[64];
    snprintf(mapsfile, sizeof(mapsfile), "/proc/%d/maps", pid);

    FILE *fp = fopen(mapsfile, "r");
    if (!fp) {
      perror(mapsfile);
      continue;
    }

    char line[4096];

    while (fgets(line, sizeof(line), fp)) {

      unsigned long start, end, offset, inode;
      unsigned maj, min;
      char perms[8];
      char path[4096] = "";
      int new_line = 0;

      int rc = sscanf(line, "%lx-%lx %7s %lx %x:%x %lu %4095[^\n]", &start,
                      &end, perms, &offset, &maj, &min, &inode, path);

      if (rc < 3)
        continue;

      char *pathname = path;
      while (*pathname == ' ')
        pathname++;

      if (perms[0] != 'r')
        continue;

      if (*pathname != '\0') {
        if (nglobs > 0 && !matches_any_glob(globs, nglobs, pathname)) {
          if (verbose > 0)
            printf("pid %d: skip %lx-%lx (%ld bytes) - does mot match globs\n",
                   pid, start, end, end - start);
          continue;
        }
        if (verbose > 1)
          printf("pid %d: %s (%ld bytes)\n", pid, pathname, end - start);
      } else {
        if (verbose > 1)
          printf("pid %d: %lx-%lx (%ld bytes)\n", pid, start, end, end - start);
      }

      unsigned long bytes_since_progress = 0;
      unsigned long iblock = 0;

      for (unsigned long addr = start; addr < end;
           addr += block_sizes[iblock % nblock_sizes], iblock++) {

        unsigned long block_end = addr + block_sizes[iblock % nblock_sizes];
        if (block_end > end)
          block_end = end;

        unsigned long len = block_end - addr;
        size_t npages = len / pagesize;

        if (npages == 0)
          continue;

        int node = nodes[iblock % nnodes];

        for (size_t i = 0; i < npages; i++) {
          pages[i] = (void *)(addr + i * pagesize);
          dest[i] = node;
        }

        if (move_pages(pid, npages, pages, dest, status, 0) < 0) {
          fprintf(stderr, "\nmove_pages failed at 0x%lx: %s\n", addr,
                  strerror(errno));
        }

        bytes_since_progress += len;

        while (bytes_since_progress >= PROGRESS_STEP) {
          new_line = 1;
          putchar('.');
          fflush(stdout);
          bytes_since_progress -= PROGRESS_STEP;
        }
      }

      if (new_line) {
        putchar('\n');
        new_line = 0;
      }
    }

    fclose(fp);
  }
  free(status);
  free(dest);
  free(pages);

  free(nodes);
  free(pids);
  for (int i = 0; i < nglobs; i++)
      free(globs[i]);
  free(globs);
  free(block_sizes);

  return 0;
}
