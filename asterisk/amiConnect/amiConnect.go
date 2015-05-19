package amiConnect

import (
	"bufio"
	"bytes"
	"errors"
	"net"
	log "github.com/ivahaev/go-logger"
	"strconv"
	"time"
)

type AMIAdapter struct {
	ip       string
	port     string
	username string
	password string

	Connected     bool
	chanActions   chan map[string]string
	chanResponses chan map[string]string
	chanEvents    chan map[string]string
	chanErr       chan error
}

func NewAMIAdapter(ip string, port string) (*AMIAdapter, error) {

	var a = new(AMIAdapter)
	a.ip = ip
	a.port = port

	conn, err := a.openConnection()
	if err != nil {
		return nil, err
	}

	chanOutStreamReader := make(chan byte)
	a.chanActions = make(chan map[string]string)

	chanErrStreamReader := streamReader(conn, chanOutStreamReader)
	a.chanErr = chanErrStreamReader
	chanQuitActionWriter := actionWriter(conn, a.chanActions, a.chanErr)
	chanOutStreamParser := streamParser(chanOutStreamReader)
	a.chanResponses, a.chanEvents = classifier(chanOutStreamParser)

	go func() {
		for {
			err := <-chanErrStreamReader
			chanQuitActionWriter <- true
			a.Connected = false

			log.Warn("TCP ERROR")

			for i := 10000000; i >= 0; i-- {

				if i == 0 {
					log.Error("Reconnect failed 10000000 times. Give up!")
				}

				log.Warn("Try reconnect in 1 second")
				time.Sleep(time.Second * 1)

				conn, err = a.openConnection()
				if err != nil {
					log.Warn("Reconnect failed! Retries remaining: " + strconv.Itoa(i))
				} else {
					chanErrStreamReader = streamReader(conn, chanOutStreamReader)
					a.chanErr = chanErrStreamReader
					chanQuitActionWriter = actionWriter(conn, a.chanActions, a.chanErr)

					_, err = a.Login(a.username, a.password)
					if err != nil {
						log.Error("Login failed!")
					}
					break
				}
			}
		}
	}()

	return a, nil
}

func (a *AMIAdapter) Login(username string, password string) (chan map[string]string, error) {

	a.username = username
	a.password = password

	var action = map[string]string{
		"Action":   "Login",
		"Username": a.username,
		"Secret":   a.password,
	}

	var result = a.Exec(action)
	if result["Response"] != "Success" {
		a.Connected = true
		return nil, errors.New("Login failed: " + result["Message"])
	}

	return a.chanEvents, nil
}

func (a *AMIAdapter) Exec(action map[string]string) map[string]string {

	a.chanActions <- action
	var response = <-a.chanResponses
	return response
}

func streamReader(conn *net.TCPConn, chanOut chan byte) (chanErr chan error) {

	chanErr = make(chan error)

	reader := bufio.NewReader(conn)

	go func() {
		for {
			b, err := reader.ReadByte()
			if err != nil {
				log.Error("AMI error:", err)
				chanErr <- err
				return
			}
			chanOut <- b
		}
	}()

	return chanErr
}

func actionWriter(conn *net.TCPConn, in chan map[string]string, chanErr chan error) (chanQuit chan bool) {

	chanQuit = make(chan bool)

	go func() {
		for {
			select {
			case action := <-in:
				{
					var data = serialize(action)
					_, err := conn.Write(data)
					if err != nil {
						chanErr <- err
						return
					}
				}
			case <-chanQuit:
				{
					return
				}
			}
		}
	}()

	return chanQuit
}

func streamParser(in chan byte) (chanOut chan map[string]string) {

	chanOut = make(chan map[string]string)

	var data = make(map[string]string)
	var wordBuf bytes.Buffer
	var key string
	var value string
	var lastByte byte
	var curByte byte
	var state = 0 // 0: key state, 1: value state

	go func() {

		for {
			lastByte = curByte
			curByte = <-in

			if curByte == ':' || curByte == '\n' {
				continue
			}

			switch state {
			case 0:
				{
					if curByte == ' ' {
						if lastByte == ':' {
							key = wordBuf.String()
							wordBuf.Reset()
							state = 1
						}
					} else if curByte == '\r' {
						if len(value) > 0 {
							chanOut <- data
							data = make(map[string]string)
						}
						wordBuf.Reset()
						key = ""
						value = ""
						lastByte = 0
						curByte = 0
						state = 0
					} else {
						wordBuf.WriteByte(curByte)
					}
				}
			case 1:
				{
					if curByte == '\r' {
						value = wordBuf.String()
						wordBuf.Reset()
						state = 0
						data[key] = value
					} else {
						wordBuf.WriteByte(curByte)
					}
				}
			}
		}
	}()

	return chanOut
}

func classifier(in chan map[string]string) (chanOutResponses chan map[string]string, chanOutEvents chan map[string]string) {

	chanOutResponses = make(chan map[string]string)
	chanOutEvents = make(chan map[string]string)

	go func() {
		for {
			data := <-in

			for d := range data {
				switch d {
				case "Response":
					chanOutResponses <- data
					break
				case "Event":
					chanOutEvents <- data
					break
				}
			}
		}
	}()

	return chanOutResponses, chanOutEvents
}

func (a *AMIAdapter) openConnection() (*net.TCPConn, error) {

	socket := a.ip + ":" + a.port

	raddr, err := net.ResolveTCPAddr("tcp", socket)
	if err != nil {
		return nil, err
	}

	conn, err := net.DialTCP("tcp", nil, raddr)
	if err != nil {
		return nil, err
	}

	return conn, err
}

func serialize(data map[string]string) []byte {

	var outBuf bytes.Buffer

	for key := range data {
		value := data[key]

		outBuf.WriteString(key)
		outBuf.WriteString(": ")
		outBuf.WriteString(value)
		outBuf.WriteString("\n")
	}
	outBuf.WriteString("\n")
	return outBuf.Bytes()
}
