package constants

const SynchronousLogger = true

const (
	LogRaftStart = iota
	// TODO: Add more log types here
)

var RaftLoggingMap = map[int]string{
	LogRaftStart: "RaftStartEvent",
	// TODO: Add more log types here
}
