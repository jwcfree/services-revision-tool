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
