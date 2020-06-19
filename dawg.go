package graph

import (
	"fmt"
	"io/ioutil"
	"time"

	"github.com/golang/snappy"
	serialize "github.com/tenta-browser/go-bitstream-ops"
)

// Constants defining graph search results
const (
	MatchFound     = 0 /// explicitly found a match
	MatchNotFound  = 1 /// explicitly failed to find a match
	MatchUncertain = 2 /// uncertain, app-spec evaluation needed (in case of payloads)
)

// Search result to string mapping
var (
	MatchStatusToString = map[int]string{MatchFound: "FOUND", MatchNotFound: "NOT FOUND", MatchUncertain: "UNCERTAIN"}
)

// Dawg - a graph of tokens (filter expressions) upon which search operations can be made
type Dawg struct {
	previousWord   string
	root           dawgNode
	uncheckedNodes []*uncheckedDawgNode
	minimizedNodes map[string]dawgNode
	serialNodes    []dawgNode
}

// SearchResult - structure holding the result of a graph search
type SearchResult struct {
	MatchStatus int
	Remainder   string
	Payload     NodePayload
}

// NewDawg - Creates a new graph with default values
func NewDawg() *Dawg {
	node := newDawgNode(false)
	list := make([]dawgNode, 1)
	list[0] = node
	return &Dawg{"", node, make([]*uncheckedDawgNode, 0), make(map[string]dawgNode), list}
}

// Serialize - Encodes by optimizing for space
func (d *Dawg) Serialize(fname string) []byte {
	b := serialize.NewBitStreamOps()
	/// save number of nodes
	/// EMIT -- 32
	b.EmitDWord(uint32(len(d.serialNodes)))
	/// calculate number of bits necessary to represent node ids (indexes)
	numBitsForID := 32 //math.Ceil(math.Log2(float64(len(d.serialNodes))))
	/// calculate number of bits necessary to represent number of edges of a node
	numBitsForEdgenum := 32 //math.Ceil(math.Log2(float64(d.maxEdges())))
	/// EMIT -- 32
	b.EmitDWord(uint32(numBitsForEdgenum))
	/// save number of bits needed to represent offsets in payload mega-string (each node will get 2 of these: `from`, and `to`)

	/// generate (and save) dictionary for all symbol occurences; for the nodes, encode only the indexes in this map
	staticDict := d.generateStaticDict()
	numBitsForDictEntry := 8 //math.Ceil(math.Log2(float64(len(staticDict))))
	/// save number of distinct edge map keys
	/// EMIT -- 8
	b.Emit(uint(len(staticDict)), 8)

	dictIndex := 0
	/// save individual dictionary items
	/// EMIT -- 8 * numDictItems
	for k := range staticDict {
		b.Emit(uint(k), 8)
		staticDict[k] = dictIndex
		dictIndex++
	}

	/// save node info, aka 1bit for final [true/false]
	///EMIT -- ceil(numNodes/8)*8
	for _, v := range d.serialNodes {
		temp := 1
		if v.isFinal() == false {
			temp = 0
		}
		b.Emit(uint(temp), 1)
	}
	/// jump head to byte boundary
	b.JumpToNextByte()
	payloadIndex := 0
	/// save edges for a given node
	for _, v := range d.serialNodes {
		b.Emit(uint(len(v.getEdges())), int(numBitsForEdgenum))
		for k, e := range v.getEdges() {
			if e == nil {
				continue
			}
			/// map key (char/symbol)
			b.Emit(uint(staticDict[k[0]]), int(numBitsForDictEntry))
			/// map value (id of node)
			b.Emit(uint(e.getID()), int(numBitsForID))
		}
		if v.isFinal() {
			if v.getPayload() != nil {
				encPayload := v.getPayload().Encode()
				b.Emit(uint(len(encPayload)), 16)
				b.JumpToNextByte()
				b.Append(encPayload)
				// log("[%d][%d][%s]\n", payloadIndex, len(encPayload), encPayload)
			} else {
				b.EmitWord(uint16(0))
				b.JumpToNextByte()
				// log("[%d][%d]\n", payloadIndex, 0)
			}
			payloadIndex++
		}
	}

	/// jump to next byte boundary
	b.JumpToNextByte()

	out := b.Buffer()
	log("Serialized graph into [%d] bytes.\n", len(out))
	comp := snappy.Encode(nil, out)
	log("Compressed size is [%d] bytes.\n", len(comp))
	if fname != "" {
		ioutil.WriteFile(fname, comp, 0644)
	}
	return comp
}

// Deserialize - Decodes a space-optimized format
func Deserialize(data []byte, dbp DawgBinaryParser, payloadClass NodePayload) *Dawg {
	if len(data) < 12 {
		dbp.OnFailed("Data malformed")
		return nil
	}

	start := time.Now()
	decoded, e := snappy.Decode(nil, data)
	if e != nil {
		log("Cannot decompress data. [%s]\n", e.Error())
	}
	b := serialize.NewBitStreamOpsReader(decoded)
	/// COLLECT -- 32
	numNodes, e := b.CollectDWord()
	if e != nil {
		log("Cannot collect number of nodes. [%s]\n", e.Error())
		return nil
	}
	log("Deserialize - numNodes is [%d]\n", numNodes)
	d := NewDawg()

	// numBitsForID := 32 //math.Ceil(math.Log2(float64(numNodes)))
	/// COLLECT -- 32
	numBitsForEdgenum, e := b.CollectDWord()
	if e != nil {
		log("Cannot collect edge num bits. [%s]\n", e.Error())
		return nil
	}
	/// COLLECT -- 8 checked
	numDictItems, e := b.Collect(8)
	log("Num dict items is %d\n", numDictItems)
	if e != nil {
		log("Cannot collect number of dictionary items")
		return nil
	}
	staticDict := make([]byte, numDictItems)
	/// COLLECT -- 8 * numDictItems checked
	for i := 0; i < int(numDictItems); i++ {
		temp, e := b.Collect(8)
		if e != nil {
			log("Cannot read dictionary item")
			return nil
		}
		staticDict[i] = byte(temp)
	}

	log("Header completed\n")
	// numBitsForDictEntry := 8 //math.Ceil(math.Log2(float64(len(staticDict))))
	/// COLLECT -- 1 (root node final bit) checked
	rootFinal, e := b.Collect(1)
	if e != nil {
		log("Cannot read root node final status. [%s]\n", e.Error())
		return nil
	}
	d.serialNodes = make([]dawgNode, numNodes)
	d.root = newDawgNodeManual(0, rootFinal == 1)
	d.serialNodes[0] = d.root
	/// COLLECT -- the rest + byte boundary checked
	for i := 1; i < int(numNodes); i++ {
		fin, e := b.Collect(1)
		if e != nil {
			log("Cannot read node finished state. [%s]\n", e.Error())
			return nil
		}
		d.serialNodes[i] = newDawgNodeManual(i, fin > 0)
	}
	/// jump head to byte boundary
	b.JumpToNextByteForRead()
	log("First batch completed [%v]\n", time.Now().Sub(start))
	payInd := 0
	for i, n := range d.serialNodes {
		edgeNum, e := b.Collect(int(numBitsForEdgenum))
		if e != nil {
			log("Cannot read number of edges for node. [%s]\n", e.Error())
			return nil
		}
		///log(">>>[%d]", edgeNum)
		debugStr := fmt.Sprintf("Node %d has %d edges [", i, edgeNum)
		for i := 0; i < int(edgeNum); i++ {
			symbol, e := b.CollectByte() //(int(numBitsForDictEntry))
			id, e2 := b.CollectDWord()   //(int(numBitsForID))
			if e != nil || e2 != nil {
				log("Cannot read edge\n")
				return nil
			}
			if symbol > numDictItems || id > numNodes {
				log("Edge parameters mismatch [%d vs %d] or [%d vs %d]\n", symbol, numDictItems, id, numNodes)
				return nil
			}
			//log("[%c-%d]", staticDict[symbol], id)
			debugStr += fmt.Sprintf("(%s,%d)", string(staticDict[symbol]), id)
			n.setEdge(string(staticDict[symbol]), d.serialNodes[id])

		}
		if n.isFinal() {
			payLen, e := b.Collect(16)
			if e != nil {
				log("Cannot collect payload length. [%s]\n", e.Error())
				return nil
			}
			b.JumpToNextByteForRead()
			if payLen > 0 {
				pay, e := b.DeAppend(int(payLen))
				if e != nil {
					log("Cannot detach payload. [%s]\n", e.Error())
					return nil
				}
				dec, e := payloadClass.Decode(pay)
				// log("[%d][%d][%s]\n", payInd, payLen, pay)
				if e != nil {
					log("Cannot decode payload. [%s]\n", e.Error())
					return nil
				}
				n.setPayload(dec)

			} else {
				// log("[%d][%d]\n", payInd, 0)
			}
			payInd++
		}
		//log("\n")
	}
	b.JumpToNextByteForRead()

	log("Graph read. Reconstruction time: [%v] -- Nodes %d vs. Edges %d\n", time.Now().Sub(start), d.nodeCountAlt(), d.edgeCountAlt())
	return d
}

/// generates a dictionary of occuring symbols in graph to reduce encoding size
func (d *Dawg) generateStaticDict() (ret map[byte]int) {
	ret = make(map[byte]int)
	for _, v := range d.getSerialNodes() {
		if v == nil {
			continue
		}
		for k := range v.getEdges() {
			if v == nil {
				continue
			}
			ret[k[0]] = 0
		}
	}
	return
}

func (d *Dawg) maxEdges() int {
	max := 0
	for _, v := range d.serialNodes {
		if v == nil {
			continue
		}
		if max < len(v.getEdges()) {
			max = len(v.getEdges())
		}
	}
	return max
}

func (d *Dawg) getSerialNodes() []dawgNode {
	return d.serialNodes
}

// Insert - Inserts a word, with an optional payload
func (d *Dawg) Insert(word string, optPayload NodePayload) bool {
	if word < d.previousWord || word == "" {
		return false
	}
	commonPrefix := 0
	for i := 0; i < minLength(word, d.previousWord); i++ {
		if word[i] != d.previousWord[i] {
			break
		}
		commonPrefix++
	}
	d.minimize(commonPrefix)
	var node dawgNode
	if len(d.uncheckedNodes) == 0 {
		node = d.root
	} else {
		node = d.uncheckedNodes[len(d.uncheckedNodes)-1].child
	}
	for _, symbol := range word[commonPrefix : len(word)-1] {
		nextNode := newDawgNode(false)
		node.getEdges()[string(symbol)] = nextNode
		d.uncheckedNodes = append(d.uncheckedNodes, &uncheckedDawgNode{parent: node, symbol: string(symbol), child: nextNode})
		node = nextNode
	}
	nextNode := newDawgNode(true)
	if optPayload != nil {
		nextNode.setPayload(optPayload)
	}
	node.getEdges()[string(word[len(word)-1])] = nextNode
	d.uncheckedNodes = append(d.uncheckedNodes, &uncheckedDawgNode{parent: node, symbol: string(word[len(word)-1]), child: nextNode})
	d.previousWord = word
	return true
}

// Finish - completes minimization of graph, and exports nodes to an array (resetting ids as indexes)
func (d *Dawg) Finish() {
	d.minimize(0)

	/// compile all usable nodes in serialnodes vector
	d.serialNodes = make([]dawgNode, len(d.minimizedNodes)+1)
	d.serialNodes[0] = d.root
	i := 1
	for _, v := range d.minimizedNodes {
		if v == nil {
			continue
		}
		d.serialNodes[i] = v
		i++
	}
	for i, v := range d.serialNodes {
		v.setID(i)
	}

	log("Finishing graph -- Nodes %d vs. Edges %d\n", d.nodeCountAlt(), d.edgeCountAlt())
}

/// minimizes unchecked nodes (tries to reuse already minimized nodes)
func (d *Dawg) minimize(toLevel int) {

	for i := len(d.uncheckedNodes) - 1; i > toLevel-1; i-- {
		u := d.uncheckedNodes[i]
		//log("{%p}", u.child)
		//log("Checking %s -- %s -- %s\n", u.parent, u.symbol, u.child)
		if _, contains := d.minimizedNodes[u.child.String()]; contains {
			//log("present\n")
			//u.parent.getEdges()[u.symbol] = d.minimizedNodes[u.child.String()]
			u.parent.setEdge(u.symbol, d.minimizedNodes[u.child.String()])
		} else {
			//log("absent\n")
			d.minimizedNodes[u.child.String()] = u.child
		}
		//log("[[%d]%d vs ", toLevel, len(d.uncheckedNodes))
		d.uncheckedNodes = d.uncheckedNodes[:i]
		//log("%d]", len(d.uncheckedNodes))
	}
}

// ExactLookup - traditional token search
func (d *Dawg) ExactLookup(word string) bool {
	node := d.root
	for _, symbol := range word {
		if _, contains := node.getEdges()[string(symbol)]; contains == false {
			return false
		}
		node = node.getEdges()[string(symbol)]
	}
	return node.isFinal()
}

// ExactLookupWithPayload -- just like exact lookup, but returns means to an additional (application specific) check with the last matching node's payload
// Walks the graph until it can't (or finds a match) and then returns the remainder of the input and the last node's payload.
func (d *Dawg) ExactLookupWithPayload(word string) *SearchResult {
	node := d.root
	// log("Searching for word [%s]\n", word)
	for i, symbol := range word {
		/// if next symbol does not match
		// log("Trying symbol [%s] ... ", string(symbol))
		if node.isFinal() {
			// log("node is final, so it depends on payload.\n")
			return &SearchResult{MatchUncertain, word[i:], node.getPayload()}
		}

		if _, contains := node.getEdges()[string(symbol)]; contains == false {
			// log("not found.\n")
			return &SearchResult{MatchNotFound, "", nil}
		}
		node = node.getEdges()[string(symbol)]
		// log("found. Possible avenues are [")
		// for sym, _ := range node.getEdges() {
		// log("%s ", string(sym))
		// }
		// log("]\n")
	}
	// log("Reached the end. last node isFinal is [%v]\n", node.isFinal())
	if node.isFinal() {
		return &SearchResult{MatchFound, "", node.getPayload()}
	} else {
		return &SearchResult{MatchNotFound, "", nil}
	}
}

func (d *Dawg) NodeCount() int {
	return len(d.minimizedNodes)
}

func (d *Dawg) nodeCountAlt() int {
	return len(d.serialNodes)
}

func (d *Dawg) edgeCountAlt() (ret int) {
	for _, e := range d.serialNodes {
		if e == nil {
			continue
		}
		ret += len(e.getEdges())
	}
	return
}

func (d *Dawg) EdgeCount() (ret int) {
	for _, v := range d.minimizedNodes {
		if v == nil {
			continue
		}
		ret += len(v.getEdges())
	}
	return
}
