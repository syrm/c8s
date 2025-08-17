package tui

type Event struct {
	Type EventType
	ActorType ActorType
	ActorID string
}

type EventType int

const (
	EventUpdate EventType = iota
	EventDelete
)

type ActorType int

const (
	ActorProject ActorType = iota
	ActorContainer
)
