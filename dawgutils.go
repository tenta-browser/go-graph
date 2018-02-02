package graph

import (
	"fmt"
	"hash/fnv"
)

const (
	SHANotMatchingError  = "Graph does not match advertised hash."
	DeserializationError = "Graph deserialization error, or mismatching library version"
	QuickCreateError     = "Cannot create QuickDawg from Dawg"
	CompactingError      = "Cannot compact QuickDawg"
)

/*
** Package level global variables
 */
var NextId int = -1
var pld PackageLevelRandomDebugger
var pDBP PlaceholderDBP
var loggingEnabled = true

func SetLoggingEnabled(b bool) {
	loggingEnabled = b
}

func log(format string, args ...interface{}) {
	if loggingEnabled == true {
		fmt.Printf(format, args...)
	}
}

func minLength(a, b string) (ret int) {
	ret = len(a)
	if len(b) < len(a) {
		ret = len(b)
	}
	return
}

func calcHash(in []byte) uint32 {
	hash := fnv.New32a()
	hash.Write(in)
	return hash.Sum32()
}

/*
** Interface for debugging
 */
type PackageLevelRandomDebugger interface {
	OnDebug(message string)
}

type ResultItem struct {
	word  string
	value int
}

// DawgBinaryParser - Interface specifically created for graph deserialization status callbacks
type DawgBinaryParser interface {
	OnCompleted(d *Dawg)
	OnFailed(cause string)
	OnDebug(message string)
}

// PlaceholderDBP - Placeholder debugging struct implementing DBP methods for local debugging
type PlaceholderDBP struct{}

func (p PlaceholderDBP) OnCompleted(d *Dawg) {}
func (p PlaceholderDBP) OnFailed(cause string) {
	log("DBP ERROR :: %s\n", cause)
}
func (p PlaceholderDBP) OnDebug(message string) {
	log("DBP DEBUG :: %s\n", message)
}
