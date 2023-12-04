// Copyright 2021 Intel Corporation. All Rights Reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package memtier

import (
	stdlog "log"
)

// Logger interface defines methods for logging at different severity levels.
type Logger interface {
	Debugf(format string, v ...interface{})
	Infof(format string, v ...interface{})
	Warnf(format string, v ...interface{})
	Errorf(format string, v ...interface{})
	Panicf(format string, v ...interface{})
	Fatalf(format string, v ...interface{})
}

type logger struct {
	*stdlog.Logger
}

const logPrefix = "memtier "

var log Logger = &logger{Logger: nil}
var logDebugMessages bool = false

// SetLogger sets a custom logger for the package.
func SetLogger(l *stdlog.Logger) {
	log = NewLoggerWrapper(l)
}

// SetLogDebug controls whether debug messages are logged.
func SetLogDebug(debug bool) {
	logDebugMessages = debug
}

// NewLoggerWrapper creates a new Logger instance that wraps the provided standard logger.
func NewLoggerWrapper(l *stdlog.Logger) Logger {
	return &logger{Logger: l}
}

// Debugf logs a debug message if debug logging is enabled.
func (l *logger) Debugf(format string, v ...interface{}) {
	if l.Logger != nil && logDebugMessages {
		l.Logger.Printf("DEBUG: "+logPrefix+format, v...)
	}
}

// Infof logs an informational message.
func (l *logger) Infof(format string, v ...interface{}) {
	if l.Logger != nil {
		l.Logger.Printf("INFO: "+logPrefix+format, v...)
	}
}

// Warnf logs a warning message.
func (l *logger) Warnf(format string, v ...interface{}) {
	if l.Logger != nil {
		l.Logger.Printf("WARN: "+logPrefix+format, v...)
	}
}

// Errorf logs an error message.
func (l *logger) Errorf(format string, v ...interface{}) {
	if l.Logger != nil {
		l.Logger.Printf("ERROR: "+logPrefix+format, v...)
	}
}

// Panicf logs a message and then panics.
func (l *logger) Panicf(format string, v ...interface{}) {
	if l.Logger != nil {
		l.Logger.Panicf(format, v...)
	}
}

// Fatalf logs a message and then exits the program.
func (l *logger) Fatalf(format string, v ...interface{}) {
	if l.Logger != nil {
		l.Logger.Fatalf(format, v...)
	}
}
