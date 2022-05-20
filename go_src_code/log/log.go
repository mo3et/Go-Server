// Copyright 2009 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package log implements a simple logging package. It defines a type, Logger,
// with methods for formatting output. It also has a predefined 'standard'
// Logger accessible through helper functions Print[f|ln], Fatal[f|ln], and
// Panic[f|ln], which are easier to use than creating a Logger manually.
// That logger writes to standard error and prints the date and time
// of each logged message.
// Every log message is output on a separate line: if the message being
// printed does not end in a newline, the logger will add one.
// The Fatal functions call os.Exit(1) after writing the log message.
// The Panic functions call panic after writing the log message.
package log

import (
	"fmt"
	"io"
	"os"
	"runtime"
	"sync"
	"sync/atomic"
	"time"
)

// These flags define which text to prefix to each log entry generated by the Logger.
// Bits are or'ed together to control what's printed.
// With the exception of the Lmsgprefix flag, there is no
// control over the order they appear (the order listed here)
// or the format they present (as described in the comments).
// The prefix is followed by a colon only when Llongfile or Lshortfile
// is specified.
// For example, flags Ldate | Ltime (or LstdFlags) produce,
//	2009/01/23 01:23:23 message
// while flags Ldate | Ltime | Lmicroseconds | Llongfile produce,
//	2009/01/23 01:23:23.123123 /a/b/c/d.go:23: message
const (
	Ldate         = 1 << iota     // the date in the local time zone: 2009/01/23
	Ltime                         // the time in the local time zone: 01:23:23
	Lmicroseconds                 // microsecond resolution: 01:23:23.123123.  assumes Ltime.
	Llongfile                     // full file name and line number: /a/b/c/d.go:23
	Lshortfile                    // final file name element and line number: d.go:23. overrides Llongfile
	LUTC                          // if Ldate or Ltime is set, use UTC rather than the local time zone
	Lmsgprefix                    // move the "prefix" from the beginning of the line to before the message
	LstdFlags     = Ldate | Ltime // initial values for the standard logger
)

// A Logger represents an active logging object that generates lines of
//一个记录器代表一个活跃的记录对象，生成的行
// output to an io.Writer. Each logging operation makes a single call to
//输出到io.writer。每个伐木操作都会打电话给
// the Writer's Write method. A Logger can be used simultaneously from
//作者的写作方法。可以同时使用记录器
// multiple goroutines; it guarantees to serialize access to the Writer.
//多个goroutines;它可以保证序列化对作者的访问。
type Logger struct {
	mu        sync.Mutex // ensures atomic writes; protects the following fields
	prefix    string     // prefix on each line to identify the logger (but see Lmsgprefix)
	flag      int        // properties
	out       io.Writer  // destination for output
	buf       []byte     // for accumulating text to write
	isDiscard int32      // atomic boolean: whether out == io.Discard
}

// New creates a new Logger. The out variable sets the
// New创建一个新的记录器。OUT变量设置
// destination to which log data will be written.
// 将编写日志数据的目的地。
// The prefix appears at the beginning of each generated log line, or
// 前缀出现在每个生成日志行的开头，或
// after the log header if the Lmsgprefix flag is provided.
// 在日志标头之后，如果提供了LMSGPREFIX标志。
// The flag argument defines the logging properties.
// 标志参数定义记录属性。
func New(out io.Writer, prefix string, flag int) *Logger {
	l := &Logger{out: out, prefix: prefix, flag: flag}
	if out == io.Discard {
		l.isDiscard = 1
	}
	return l
}

// SetOutput sets the output destination for the logger.
// SETOUTPUT设置记录器的输出目标。
func (l *Logger) SetOutput(w io.Writer) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.out = w
	isDiscard := int32(0)
	if w == io.Discard {
		isDiscard = 1
	}
	atomic.StoreInt32(&l.isDiscard, isDiscard)
}

var std = New(os.Stderr, "", LstdFlags)

// Default returns the standard logger used by the package-level output functions.
// 默认值返回软件包级输出功能使用的标准日志仪。
func Default() *Logger { return std }

// Cheap integer to fixed-width decimal ASCII. Give a negative width to avoid zero-padding.
// 便宜的整数到固定宽度小数ASCII。给出负宽度以避免零盖。
func itoa(buf *[]byte, i int, wid int) {
	// Assemble decimal in reverse order.
	var b [20]byte
	bp := len(b) - 1
	for i >= 10 || wid > 1 {
		wid--
		q := i / 10
		b[bp] = byte('0' + i - q*10)
		bp--
		i = q
	}
	// i < 10
	b[bp] = byte('0' + i)
	*buf = append(*buf, b[bp:]...)
}

// formatHeader writes log header to buf in following order:
//   * l.prefix (if it's not blank and Lmsgprefix is unset),
//   * date and/or time (if corresponding flags are provided),
//   * file and line number (if corresponding flags are provided),
//   * l.prefix (if it's not blank and Lmsgprefix is set).

// formatHeader 按以下顺序将日志头写入 buf：
// * l.prefix（如果它不为空且 Lmsgprefix 未设置），
// * 日期和/或时间（如果提供了相应的标志），
// * 文件和行号（如果提供了相应的标志），
// * l.prefix（如果它不是空白并且设置了 Lmsgprefix）。
func (l *Logger) formatHeader(buf *[]byte, t time.Time, file string, line int) {
	if l.flag&Lmsgprefix == 0 {
		*buf = append(*buf, l.prefix...)
	}
	if l.flag&(Ldate|Ltime|Lmicroseconds) != 0 {
		if l.flag&LUTC != 0 {
			t = t.UTC()
		}
		if l.flag&Ldate != 0 {
			year, month, day := t.Date()
			itoa(buf, year, 4)
			*buf = append(*buf, '/')
			itoa(buf, int(month), 2)
			*buf = append(*buf, '/')
			itoa(buf, day, 2)
			*buf = append(*buf, ' ')
		}
		if l.flag&(Ltime|Lmicroseconds) != 0 {
			hour, min, sec := t.Clock()
			itoa(buf, hour, 2)
			*buf = append(*buf, ':')
			itoa(buf, min, 2)
			*buf = append(*buf, ':')
			itoa(buf, sec, 2)
			if l.flag&Lmicroseconds != 0 {
				*buf = append(*buf, '.')
				itoa(buf, t.Nanosecond()/1e3, 6)
			}
			*buf = append(*buf, ' ')
		}
	}
	if l.flag&(Lshortfile|Llongfile) != 0 {
		if l.flag&Lshortfile != 0 {
			short := file
			for i := len(file) - 1; i > 0; i-- {
				if file[i] == '/' {
					short = file[i+1:]
					break
				}
			}
			file = short
		}
		*buf = append(*buf, file...)
		*buf = append(*buf, ':')
		itoa(buf, line, -1)
		*buf = append(*buf, ": "...)
	}
	if l.flag&Lmsgprefix != 0 {
		*buf = append(*buf, l.prefix...)
	}
}

// Output writes the output for a logging event. The string s contains
// the text to print after the prefix specified by the flags of the
// Logger. A newline is appended if the last character of s is not
// already a newline. Calldepth is used to recover the PC and is
// provided for generality, although at the moment on all pre-defined
// paths it will be 2.

// 输出写入日志事件的输出。 字符串 s 包含
// 在由标志指定的前缀之后打印的文本
// 记录器。 如果 s 的最后一个字符不是，则附加换行符
// 已经是换行符了。 Calldepth 用于恢复 PC 并且是
// 提供一般性，尽管目前所有预定义的
// 路径将是 2。

func (l *Logger) Output(calldepth int, s string) error {
	now := time.Now() // get this early.
	var file string
	var line int
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.flag&(Lshortfile|Llongfile) != 0 {
		// Release lock while getting caller info - it's expensive.
		l.mu.Unlock()
		var ok bool
		_, file, line, ok = runtime.Caller(calldepth)
		if !ok {
			file = "???"
			line = 0
		}
		l.mu.Lock()
	}
	l.buf = l.buf[:0]
	l.formatHeader(&l.buf, now, file, line)
	l.buf = append(l.buf, s...)
	if len(s) == 0 || s[len(s)-1] != '\n' {
		l.buf = append(l.buf, '\n')
	}
	_, err := l.out.Write(l.buf)
	return err
}

// Printf calls l.Output to print to the logger.
// printf调用l.Output以打印到记录器。
// Arguments are handled in the manner of fmt.Printf.
// 参数以fmt.printf的方式处理。
func (l *Logger) Printf(format string, v ...any) {
	if atomic.LoadInt32(&l.isDiscard) != 0 {
		return
	}
	l.Output(2, fmt.Sprintf(format, v...))
}

// Print calls l.Output to print to the logger.
// 打印调用l.Output以打印到记录器。
// Arguments are handled in the manner of fmt.Print.
// 参数以fmt.print的方式处理。
func (l *Logger) Print(v ...any) {
	if atomic.LoadInt32(&l.isDiscard) != 0 {
		return
	}
	l.Output(2, fmt.Sprint(v...))
}

// Println calls l.Output to print to the logger.
// println调用l.Output打印到记录器。
// Arguments are handled in the manner of fmt.Println.
// 参数以fmt.println的方式处理。
func (l *Logger) Println(v ...any) {
	if atomic.LoadInt32(&l.isDiscard) != 0 {
		return
	}
	l.Output(2, fmt.Sprintln(v...))
}

// Fatal is equivalent to l.Print() followed by a call to os.Exit(1).
func (l *Logger) Fatal(v ...any) {
	l.Output(2, fmt.Sprint(v...))
	os.Exit(1)
}

// Fatalf is equivalent to l.Printf() followed by a call to os.Exit(1).
func (l *Logger) Fatalf(format string, v ...any) {
	l.Output(2, fmt.Sprintf(format, v...))
	os.Exit(1)
}

// Fatalln is equivalent to l.Println() followed by a call to os.Exit(1).
func (l *Logger) Fatalln(v ...any) {
	l.Output(2, fmt.Sprintln(v...))
	os.Exit(1)
}

// Panic is equivalent to l.Print() followed by a call to panic().
func (l *Logger) Panic(v ...any) {
	s := fmt.Sprint(v...)
	l.Output(2, s)
	panic(s)
}

// Panicf is equivalent to l.Printf() followed by a call to panic().
func (l *Logger) Panicf(format string, v ...any) {
	s := fmt.Sprintf(format, v...)
	l.Output(2, s)
	panic(s)
}

// Panicln is equivalent to l.Println() followed by a call to panic().
func (l *Logger) Panicln(v ...any) {
	s := fmt.Sprintln(v...)
	l.Output(2, s)
	panic(s)
}

// Flags returns the output flags for the logger.
// The flag bits are Ldate, Ltime, and so on.

// Flags 返回记录器的输出标志。
// 标志位是 Ldate、Ltime 等。
func (l *Logger) Flags() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.flag
}

// SetFlags sets the output flags for the logger.
// setFlags设置对数字机的输出标志。
// The flag bits are Ldate, Ltime, and so on.

// SetFlags 设置记录器的输出标志。
// 标志位是 Ldate、Ltime 等。
func (l *Logger) SetFlags(flag int) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.flag = flag
}

// Prefix returns the output prefix for the logger.
// 前缀返回记录器的输出前缀。
func (l *Logger) Prefix() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.prefix
}

// SetPrefix sets the output prefix for the logger.
// setPrefix设置记录器的输出前缀。
func (l *Logger) SetPrefix(prefix string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.prefix = prefix
}

// Writer returns the output destination for the logger.
// Writer返回记录器的输出目的地。
func (l *Logger) Writer() io.Writer {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.out
}

// SetOutput sets the output destination for the standard logger.
// SETOUTPUT设置标准记录器的输出目标。
func SetOutput(w io.Writer) {
	std.SetOutput(w)
}

// Flags returns the output flags for the standard logger.
// 标志返回标准记录器的输出标志。
// The flag bits are Ldate, Ltime, and so on.
// flag bits是日期，时间等。
func Flags() int {
	return std.Flags()
}

// SetFlags sets the output flags for the standard logger.
// SetFlags设置标准日志仪的输出标志。
// The flag bits are Ldate, Ltime, and so on.
// flag bits是日期，时间等。
func SetFlags(flag int) {
	std.SetFlags(flag)
}

// Prefix returns the output prefix for the standard logger.
// 前缀返回标准记录器的输出前缀。
func Prefix() string {
	return std.Prefix()
}

// SetPrefix sets the output prefix for the standard logger.
// setPrefix设置标准日志仪的输出前缀。
func SetPrefix(prefix string) {
	std.SetPrefix(prefix)
}

// Writer returns the output destination for the standard logger.
// Writer返回标准记录器的输出目的地。
func Writer() io.Writer {
	return std.Writer()
}

// These functions write to the standard logger.
// 这些功能写入标准记录器。
// Print calls Output to print to the standard logger.
// 打印呼叫输出以打印到标准记录器。
// Arguments are handled in the manner of fmt.Print.
// 参数以fmt.print的方式处理。
func Print(v ...any) {
	if atomic.LoadInt32(&std.isDiscard) != 0 {
		return
	}
	std.Output(2, fmt.Sprint(v...))
}

// Printf calls Output to print to the standard logger.
// printf调用输出以打印到标准记录器。
// Arguments are handled in the manner of fmt.Printf.
// 参数以fmt.printf的方式处理。
func Printf(format string, v ...any) {
	if atomic.LoadInt32(&std.isDiscard) != 0 {
		return
	}
	std.Output(2, fmt.Sprintf(format, v...))
}

// Println calls Output to print to the standard logger.
// println调用输出以打印到标准记录器。
// Arguments are handled in the manner of fmt.Println.
//参数以fmt.println的方式处理。
func Println(v ...any) {
	if atomic.LoadInt32(&std.isDiscard) != 0 {
		return
	}
	std.Output(2, fmt.Sprintln(v...))
}

// Fatal is equivalent to Print() followed by a call to os.Exit(1).
//致命等于print（），然后致电OS.EXIT（1）。
func Fatal(v ...any) {
	std.Output(2, fmt.Sprint(v...))
	os.Exit(1)
}

// Fatalf is equivalent to Printf() followed by a call to os.Exit(1).
// FATALF等效于printf（），然后呼叫OS.EXIT（1）。
func Fatalf(format string, v ...any) {
	std.Output(2, fmt.Sprintf(format, v...))
	os.Exit(1)
}

// Fatalln is equivalent to Println() followed by a call to os.Exit(1).
// FATALLN等同于println（），然后致电OS.EXIT（1）。
func Fatalln(v ...any) {
	std.Output(2, fmt.Sprintln(v...))
	os.Exit(1)
}

// Panic is equivalent to Print() followed by a call to panic().
//恐慌等同于print（），然后是panic（）的呼叫。
func Panic(v ...any) {
	s := fmt.Sprint(v...)
	std.Output(2, s)
	panic(s)
}

// Panicf is equivalent to Printf() followed by a call to panic().
// panicf等效于printf（），然后呼叫panic（）。
func Panicf(format string, v ...any) {
	s := fmt.Sprintf(format, v...)
	std.Output(2, s)
	panic(s)
}

// Panicln is equivalent to Println() followed by a call to panic().
// panicln等于println（），然后呼叫panic（）。
func Panicln(v ...any) {
	s := fmt.Sprintln(v...)
	std.Output(2, s)
	panic(s)
}

// Output writes the output for a logging event. The string s contains
// the text to print after the prefix specified by the flags of the
// Logger. A newline is appended if the last character of s is not
// already a newline. Calldepth is the count of the number of
// frames to skip when computing the file name and line number
// if Llongfile or Lshortfile is set; a value of 1 will print the details
// for the caller of Output.

// 输出写入日志事件的输出。 字符串 s 包含
// 在由标志指定的前缀之后打印的文本
// 记录器。 如果 s 的最后一个字符不是，则附加换行符
// 已经是换行符了。 calldepth 是计数的数量
// 计算文件名和行号时要跳过的帧
// 如果设置了 Llongfile 或 Lshortfile； 值为 1 将打印详细信息
// 对于输出的调用者。

func Output(calldepth int, s string) error {
	return std.Output(calldepth+1, s) // +1 for this frame.
}
