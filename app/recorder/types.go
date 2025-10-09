package main

const (
	SourceUser = "USER"
	SourceAI   = "AI"
)

type QA struct {
	Q string `json:"q"`
	A string `json:"a"`
}
