#!/bin/bash

memtierd-setup

# Create swap
zram-install
zram-swap off
zram-swap 2G

MEME_BS=1G
memtierd-meme-start

# 1. Find the 1G address range from meme (pages -pid MEME_PID -pr 20)
# 2. Verify that the whole range is found as a single range when PagemapBitPresent is ignored (no pagemap bit scan)
# 4. swap -out -pid MEME_PID -ranges (a-range-in-the-middle) and verify swapped out range is found with PagemapBitPresent:false
# 5. Verify to find ... -- swapout-start and swapout-end -- all-end ranges with PagemapBit:true
# 6. Verify to find full start-end region when PagemapBitPresent is ignored also when the middle range is swapped out.

MEMTIERD_YAML=""
memtierd-start
memtierd-command "pages -pid ${MEME_PID} -pr 50"
echo "# Find the address range of the ${MEME_BS} allocation"
PARSED_AR=$(grep -E '[0-9a-f]+-' <<< "$COMMAND_OUTPUT" | sed -E 's/[- ()]/ /g' | while read -r start end size remainder; do
                if [[ -n "$size" ]] && (( ${size} > $((1024*1024*1000)) )); then
                    startmid=$((16#${start} + 1024*1024*50))
                    endmid=$((startmid + 1024*1024*100))
                    printf "startall=%s\nendall=%s\nstartmid=%x\nendmid=%x\n" "$start" "$end" "$startmid" "$endmid"
                fi
            done)
if [[ "$PARSED_AR" != "startall="* ]]; then
    error "failed to find the start and end addresses of the 1G block"
fi
echo -e "$PARSED_AR"
eval "$PARSED_AR"

memtierd-command "tracker -create finder -config {\\\"ReportAccesses\\\":42} -config-dump -start ${MEME_PID} -counters"
echo -e "\n# Expect a=42 ... $startall-$endall to be found from the counters:"
if ! grep "a=42 .*$startall-$endall" <<< "$COMMAND_OUTPUT"; then
    error "cannot find the large block with 42 accesses from counters"
fi

echo ""
echo "# Swap out an address range and verify that the finder finds pages that are not present"
memtierd-command "swap -pid ${MEME_PID} -out -ranges $startmid-$endmid"
memtierd-command "tracker -create finder -config {\\\"PagemapBitPresent\\\":false} -config-dump -start ${MEME_PID} -counters"
echo -e "\n# Expect a=0 ... $startmid-$endmid to be found from the counters:"
if ! grep "a=0 .*$startmid-$endmid" <<< "$COMMAND_OUTPUT"; then
    error "cannot find pages that should have been marked as not present"
fi
echo -e "\n# Expect $startall-$startmid and $endmid-$endall are found when looking for pages present in memory"
memtierd-command "tracker -create finder -config {\\\"PagemapBitPresent\\\":true} -config-dump -start ${MEME_PID} -counters"
echo -e "\n# Expect a=0 ...-$startmid and $endmid-... to be found from the counters:"
# Note that we cannot assume that $startall-$startmid would be all present in memory
# because (especially when THP is not used) this range may be allocated to the application
# but it may contain copy-on-write pages that are neither present nor swapped and pfn=0.
# Pages from start address of meme's array should all exist, but Go allocates some extra
# memory that contain unused pages before the array.
if ! grep -- "-$startmid" <<< "$COMMAND_OUTPUT"; then
    error "cannot find pages ...-$startmid"
fi
if ! grep "$endmid-" <<< "$COMMAND_OUTPUT"; then
    error "cannot find pages $endmid-..."
fi
memtierd-command "tracker -create finder -config \"{\\\"ReportAccesses\\\":2, \\\"PagemapBitSoftDirty\\\":true}\" -config-dump -start ${MEME_PID} -counters"
echo -e "\n# Expect $startall-$endall reported even if $startmid-$endmid is not present and scanning softdirty pages"
CHECK_RESULT=$(grep -E 'ranges=' <<< "$COMMAND_OUTPUT" | sed -e 's/.*ranges=//g' -e 's/[- ()]/ /g' | while read -r start end size remainder; do
                if (( "0x$start" <=  "0x$startall" )) && (( "0x$end" >= "0x$endall" )); then
                    echo "range $start-$end contains $startall-$endall"
                fi
            done)
if [[ "$CHECK_RESULT" != "range"* ]]; then
    error "cannot find full block"
fi
echo "$CHECK_RESULT"

memtierd-stop
memtierd-meme-stop
