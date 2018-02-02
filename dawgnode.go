package graph

import (
	"sort"
	"strconv"
)

type dawgNode interface {
	getID() int
	setID(int)
	isFinal() bool
	setFinal(bool)
	getEdges() map[string]dawgNode
	setEdges(map[string]dawgNode)
	setEdge(string, dawgNode)
	setPayload(p NodePayload)
	getPayload() NodePayload
	String() string
	hash() int32
	equals(dawgNode) bool
}

type intermediaryNode struct {
	id    int
	final bool
	edges map[string]dawgNode
}

type finalNode struct {
	*intermediaryNode
	payload NodePayload
}

// NodePayload - Interface used to store custom data in the graph, linked to a token
type NodePayload interface {
	Encode() []byte
	Decode([]byte) (NodePayload, error)
	String() string
}

/// DummyPayload -- use this struct to inherit default interface implementations
type DummyPayload struct {
}

/// implement dummy payload interface functions
func (d *DummyPayload) Encode() []byte {
	return nil
}

func (d *DummyPayload) String() string {
	return ""
}

func (d *DummyPayload) Decode(enc []byte) (NodePayload, error) {
	return NodePayload(nil), nil
}

/// implement node interface methods
func (i *intermediaryNode) setPayload(p NodePayload) {
	return
}

func (i *intermediaryNode) getPayload() NodePayload {
	return nil
}

func (i *intermediaryNode) getID() int {
	return i.id
}

func (i *intermediaryNode) setID(id int) {
	i.id = id
}

func (i *intermediaryNode) isFinal() bool {
	return i.final
}

func (i *intermediaryNode) setFinal(f bool) {
	i.final = f
}

func (i *intermediaryNode) getEdges() map[string]dawgNode {
	return i.edges
}

func (i *intermediaryNode) setEdges(m map[string]dawgNode) {
	i.edges = m
}

func (i *intermediaryNode) setEdge(k string, v dawgNode) {
	i.edges[k] = v
}

func (i *intermediaryNode) String() (s string) {
	if i.isFinal() {
		s += "1"
	} else {
		s += "0"
	}
	temp := make([]string, len(i.getEdges()))
	j := 0
	for k := range i.getEdges() {
		temp[j] = k
		j++
	}
	sort.Strings(temp)

	for _, k := range temp {
		if i.getEdges()[k] == nil {
			continue
		}
		s += "_" + k
		s += "_" + strconv.Itoa(i.getEdges()[k].getID())
	}
	return s
}

func (f *finalNode) String() (s string) {
	s += f.intermediaryNode.String()
	if f.payload != nil {
		s += f.payload.String()
	}
	return
}

func (f *finalNode) setPayload(p NodePayload) {
	f.payload = p
}

func (f *finalNode) getPayload() NodePayload {
	return f.payload
}

func (i *intermediaryNode) hash() (h int32) {
	return int32(calcHash([]byte(i.String())))
}

func (f *finalNode) hash() (h int32) {
	return int32(calcHash([]byte(f.String())))
}

func (i *intermediaryNode) equals(j dawgNode) (ret bool) {
	ret = false
	if i.String() == j.String() {
		ret = true
	}
	return
}

func (f *finalNode) equals(j dawgNode) (ret bool) {
	ret = false
	if f.String() == j.String() {
		ret = true
	}
	return
}

func newDawgNode(isFinal bool) dawgNode {
	NextId++
	i := &intermediaryNode{NextId, isFinal, make(map[string]dawgNode)}
	// log("creating node [%v] - [%p][%s]\n", isFinal, i, i)
	if !isFinal {
		return i
	}
	z := &finalNode{i, nil}
	// log("creating node [%v] - [%p][%s]\n", isFinal, z.intermediaryNode, z)
	return z
}

func newDawgNodeManual(id int, isFinal bool) dawgNode {
	i := &intermediaryNode{id, isFinal, make(map[string]dawgNode)}
	if !isFinal {
		return i
	}
	return &finalNode{i, nil}
}

type uncheckedDawgNode struct {
	parent, child dawgNode
	symbol        string
}
