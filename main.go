// Package main is a self-contained HTTP pub-sub server. All requests are made in the context of a single shared topic. Subscriptions are created implicitly when a /pull or /ack request is made on a subscription id. A subscription can be canceled (highly recommended!) using the /unsub operation.
package main

import (
	"container/heap"
	"encoding/json"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"sync"
)

// A MessageQueue keeps track of unacked messages. Using a map set for this would be easier but would require tons of sorting ops.
type MessageQueue []uint64

// Len implements the heap interface
func (q MessageQueue) Len() int { return len(q) }

// Less implements the heap interface.
func (q MessageQueue) Less(i, j int) bool {
	return q[i] < q[j]
}

// Swap implements the heap interface.
func (q MessageQueue) Swap(i, j int) {
	q[i], q[j] = q[j], q[i]
}

// Push implements the heap Interface.
func (q *MessageQueue) Push(x interface{}) {
	item := x.(uint64)
	*q = append(*q, item)
}

// Pop implements the heap  interface.
func (q *MessageQueue) Pop() interface{} {
	n := len(*q)
	item := (*q)[n-1]
	*q = (*q)[0 : n-1]
	return item
}

// Topic holds state information for a (the) topic.
type Topic struct {
	sync.RWMutex
	Name       string
	NextMesgID uint64
}

// A Subscription keeps track of received messages that have not yet been acknowledged for a given subscription id.
type Subscription struct {
	sync.RWMutex
	Name    string
	UnAcked MessageQueue
}

var subs = make(map[string]*Subscription)
var subsMu = sync.RWMutex{}

var topic = &Topic{Name: "<default-topic>"}

var dataDirname = flag.String("data-dir", ".", "Root directory for data storage")
var host = flag.String("host", "127.0.0.1", "HTTP host name to bind to")
var port = flag.Int("port", 8080, "HTTP port to bind to")

var validSubRegexp = regexp.MustCompile(`^([a-zA-Z])([a-zA-Z0-9_-])*$`)

// GetSubscription gets a sub by name and creates a new one if it doesn't exist.
func GetSubscription(w http.ResponseWriter, r *http.Request) (*Subscription, bool) {
	name := r.Form.Get("sub")
	if !validSubRegexp.MatchString(name) {
		w.WriteHeader(http.StatusBadRequest)
		return nil, false
	}
	subsMu.Lock() // Yes, we want the exclusive write lock
	defer subsMu.Unlock()
	sub, ok := subs[name]
	if ok {
		return sub, true
	}

	sub = &Subscription{
		Name:    name,
		UnAcked: make(MessageQueue, 0),
	}
	heap.Init(&sub.UnAcked)
	subs[name] = sub
	return sub, true
}

// DestroySubscription will ensure that state is no longer accumulated for the given sub.
func DestroySubscription(sub *Subscription) {
	subsMu.Lock()
	defer subsMu.Unlock()
	delete(subs, sub.Name)
}

// CreateMessageIds will increment the topic's next message id by nMessage and add the added ids to the unacknowledged message list for that topic.
func CreateMessageIds(nMessage int) uint64 {
	topic.Lock()
	defer topic.Unlock()
	baseID := topic.NextMesgID
	topic.NextMesgID += uint64(nMessage)
	return baseID
}

// FindUnAckedMessageIds returns up to maxMessages message ids by examining the the unacked messages priority queue of associated with subscription.
func FindUnAckedMessageIds(sub *Subscription, maxMessages int) []uint64 {
	sub.RLock()
	defer sub.RUnlock()
	n := maxMessages
	if len(sub.UnAcked) < maxMessages {
		n = len(sub.UnAcked)
	}
	messages := make([]uint64, n)
	copy(messages, sub.UnAcked[0:n])
	return messages
}

// PutMessages stores messages permanently and assigns them (previously created) message ids beginning at baseID.
func PutMessages(messages []string, baseID uint64) error {
	for i, m := range messages {
		filename := filepath.Join(*dataDirname, fmt.Sprint(baseID+uint64(i)))
		if err := ioutil.WriteFile(filename, []byte(m), 0644); err != nil {
			log.Printf("In PutMessages: %v", err)
			return err
		}
	}
	for _, sub := range subs {
		sub.Lock()
		for i := baseID; i < baseID+uint64(len(messages)); i++ {
			heap.Push(&sub.UnAcked, i)
		}
		sub.Unlock()
	}
	return nil
}

// GetMessages returns a map of the topic message bodies associated with ids.
func GetMessages(ids []uint64) (map[uint64]string, error) {
	messages := make(map[uint64]string)
	for _, id := range ids {
		filename := filepath.Join(*dataDirname, fmt.Sprint(id))
		bs, err := ioutil.ReadFile(filename)
		if err != nil {
			log.Printf("In GetMessages: %v", err)
			return messages, err
		}
		messages[id] = string(bs)
	}
	return messages, nil
}

// AckMessages removes ids from the topic priority queue of unacked messages.
func AckMessages(ids []uint64, sub *Subscription) {
	idMap := make(map[uint64]bool)
	for _, k := range ids {
		idMap[k] = true
	}
	nID := 0
	for range ids {
		nID++
	}

	sub.Lock()
	defer sub.Unlock()
	// We go back to front so we don't disturb lower indicies.
	for i := len(sub.UnAcked) - 1; i >= 0; i-- {
		if nID == 0 {
			// User wanted to ack nID (unique) ids, we're done if we've accounted for them all.
			return
		}
		if idMap[sub.UnAcked[i]] {
			heap.Remove(&sub.UnAcked, i)
			nID--
		}
	}
}

// JSONResponse  is a type that gives shape to our HTTP response JSON.
type JSONResponse struct {
	NMessage int               `json:"n_messages"`
	Messages map[uint64]string `json:"messages"`
}

func marshall(messages map[uint64]string) ([]byte, error) {
	return json.Marshal(JSONResponse{len(messages), messages})
}

func main() {
	flag.Parse()
	if err := os.MkdirAll(*dataDirname, 0755); err != nil {
		log.Fatalf("While creating data directory: %v", err)
	}

	http.HandleFunc("/send", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		r.ParseForm()
		messages := r.Form["message"]
		baseID := CreateMessageIds(len(messages))
		if err := PutMessages(messages, baseID); err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
	})

	http.HandleFunc("/unsub", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		r.ParseForm()
		sub, ok := GetSubscription(w, r)
		if !ok {
			return
		}
		DestroySubscription(sub)
		w.WriteHeader(http.StatusOK)
	})

	http.HandleFunc("/pull", func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		sub, ok := GetSubscription(w, r)
		if !ok {
			return
		}
		nMessageString := r.Form.Get("n")
		nMessage, err := strconv.Atoi(nMessageString)
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		messageIDs := FindUnAckedMessageIds(sub, nMessage)
		messages, err := GetMessages(messageIDs)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		bs, err := marshall(messages)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
		w.Write(bs)
		w.Write([]byte("\n"))
	})

	http.HandleFunc("/ack", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		r.ParseForm()
		sub, ok := GetSubscription(w, r)
		if !ok {
			return
		}

		messageIDs := make([]uint64, 0, 16)
		for _, idString := range r.Form["id"] {
			id, err := strconv.ParseUint(idString, 10, 64)
			if err != nil {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			messageIDs = append(messageIDs, uint64(id))
		}
		AckMessages(messageIDs, sub)
	})

	addr := fmt.Sprintf("%s:%d", *host, *port)
	log.Printf("Storing data in %s", *dataDirname)
	log.Printf("Starting listener on %s", addr)
	log.Fatal(http.ListenAndServe(addr, nil))
}
