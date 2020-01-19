package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"strconv"
	"strings"
	"sync/atomic"
)

var (
	seqCounter    int64
	responseChans = make(map[int64]chan Response)
)

type ProtocolMessage struct {
	Seq  int64  `json:"seq"`
	Type string `json:"type"`
}

type Request struct {
	ProtocolMessage             // Type must be "request"
	Command         string      `json:"command"`
	Arguments       interface{} `json:"arguments,omitempty"`
}

type Response struct {
	ProtocolMessage
	RequestSeq int64           `json:"request_seq"`
	Success    bool            `json:"success"`
	Command    string          `json:"command"`
	Message    string          `json:"message"`
	Body       json.RawMessage `json:"body"`
}

type Capabilities struct {
	SupportsConfigurationDoneRequest  bool `json:""`
	SupportsFunctionBreakpoints       bool `json:""`
	SupportsConditionalBreakpoints    bool `json:""`
	SupportsHitConditionalBreakpoints bool `json:""`
	SupportsEvaluateForHovers         bool `json:""`
	// ExceptionBreakpointFilters        []ExceptionBreakpointsFilter `json:""`
	SupportsStepBack             bool     `json:""`
	SupportsSetVariable          bool     `json:""`
	SupportsRestartFrame         bool     `json:""`
	SupportsGotoTargetsRequest   bool     `json:""`
	SupportsStepInTargetsRequest bool     `json:""`
	SupportsCompletionsRequest   bool     `json:""`
	CompletionTriggerCharacters  []string `json:""`
	SupportsModulesRequest       bool     `json:""`
	// TODO: more
}

type InitializeRequestArgs struct {
	ClientID   string `json:"clientID,omitempty"`
	ClientName string `json:"clientName,omitempty"`
	AdapterID  string `json:"adapterID"`
	Locale     string `json:"locale,omitempty"`
	// TODO: figure out how to handle these bools
	// LinesStartAt1   bool   `json:"linesStartAt1,omitempty"`
	// ColumnsStartAt1 bool   `json:"columnsStartAt1,omitempty"`
	// also add the rest
}

func NewRequest() ProtocolMessage {
	return ProtocolMessage{Seq: atomic.AddInt64(&seqCounter, 1), Type: "request"}
}

func InitializeRequest(args InitializeRequestArgs) Request {
	return Request{
		ProtocolMessage: NewRequest(),
		Command:         "initialize",
		Arguments:       args,
	}
}

func listen(c net.Conn) {
	r := bufio.NewReader(c)
	for {
		headers := make(map[string]string)
		for {
			// Technically we need to look for \r\n, but this should catch the \r too, we just need to trim it off.
			data, err := r.ReadBytes('\n')
			if err != nil {
				if err == io.EOF {
					return
				}
				log.Fatalf("failed to read line: %s", err)
			}
			line := string(bytes.TrimSpace(data))
			if len(line) == 0 {
				break
			}
			parts := strings.Split(line, ":")
			headers[strings.TrimSpace(parts[0])] = strings.TrimSpace(parts[1])
		}

		if headers["Content-Length"] == "" {
			log.Printf("warning: no Content-Length header")
			continue
		}

		contentLength, err := strconv.Atoi(headers["Content-Length"])
		if err != nil {
			log.Fatalf("bad Content-Length: %s", err)
		}

		body := make([]byte, contentLength)
		if _, err := io.ReadFull(r, body); err != nil {
			if err == io.EOF {
				return
			}
			log.Fatalf("failed to read body: %s", err)
		}

		var resp Response
		if err := json.Unmarshal(body, &resp); err != nil {
			log.Fatalf("failed to unmarshal response body")
		}

		if ch, ok := responseChans[resp.RequestSeq]; ok {
			ch <- resp
			close(ch)
			delete(responseChans, resp.RequestSeq)
		}
		// do anything if there is no response channel?
	}
}

func sendMessage(c net.Conn, msg interface{}) {
	b, err := json.Marshal(msg)
	if err != nil {
		log.Printf("failed to send message: %s", err)
		return
	}
	fmt.Fprintf(c, "Content-Length: %d\r\n", len(b))
	fmt.Fprint(c, "\r\n")
	c.Write(b)
}

func initialize(c net.Conn) Capabilities {
	req := InitializeRequest(InitializeRequestArgs{
		AdapterID: "dap-cli",
	})
	responseChans[req.Seq] = make(chan Response)
	sendMessage(c, req)
	resp := <-responseChans[req.Seq]
	if !resp.Success {
		log.Println(resp)
		log.Fatal("initialization failed")
	}
	var caps Capabilities
	if err := json.Unmarshal(resp.Body, &caps); err != nil {
		log.Fatalf("failed to read capabilities: %s", err)
	}
	return caps
}

func handleInput() {
	scanner := bufio.NewScanner(os.Stdin)
	for {
		fmt.Print("> ")
		os.Stdout.Sync()
		if !scanner.Scan() {
			break
		}
		// TODO: process scanner.Text()
	}
	if err := scanner.Err(); err != nil {
		log.Printf("input scanner exited with error: %s", err)
	}
}

func main() {
	addr := os.Args[1]
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		log.Fatalf("failed to dial %s: %s", addr, err)
	}
	go listen(conn)
	caps := initialize(conn)
	fmt.Printf("capabilities: %+v\n", caps)

	handleInput()
}
