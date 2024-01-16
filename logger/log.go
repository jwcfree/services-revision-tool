package logger

import (
	"os"
	"sources/config"

	"github.com/sirupsen/logrus"
)

var (
	Log = logrus.New()
)

func InitLog() {
	var l logrus.Level
	Log.SetOutput(os.Stdout)
	Log.SetFormatter(&logrus.TextFormatter{
		FullTimestamp:          true,
		DisableLevelTruncation: true,
	})
	switch config.LogLevel {
	case "WARN":
		l = logrus.WarnLevel
	case "INFO":
		l = logrus.InfoLevel
	case "DEBUG":
		l = logrus.DebugLevel
	case "TRACE":
		l = logrus.TraceLevel
	case "ERROR":
		l = logrus.ErrorLevel
	default:
		{
			l = logrus.InfoLevel
		}
	}
	Log.SetLevel(l)
}

// Error записывает сообщение об ошибке в лог
func Error(args ...interface{}) {
    Log.Error(args...)
}

// Вы также можете добавить дополнительные функции для других уровней ведения журнала, например:
func Info(args ...interface{}) {
    Log.Info(args...)
}

func Debug(args ...interface{}) {
    Log.Debug(args...)
}

func Warn(args ...interface{}) {
    Log.Warn(args...)
}

func Fatal(args ...interface{}) {
    Log.Fatal(args...)
}

func Trace(args ...interface{}) {
    Log.Trace(args...)
}