package common

import "time"

type Point struct {
	X int
	Y int
}

type Message struct {
	Author    string
	Body      string
	System    bool
	CreatedAt time.Time
}

type Player struct {
	ID          string
	Name        string
	Position    Point
	IsAdmin     bool
	Color       string
	ConnectedAt time.Time
}

type TickMsg struct {
	Time time.Time
}

type RefreshMsg struct{}

type MoveMsg struct {
	Direction string
}

type PlayerJoinedMsg struct {
	PlayerID string
	Name     string
	Position Point
	Color    string
}

type PlayerLeftMsg struct {
	PlayerID string
}