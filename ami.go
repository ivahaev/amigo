package amigo

import (
	"bufio"
	"bytes"
	"errors"
	"net"
	"sync"
	"time"

	log "github.com/ivahaev/go-logger"
)

type amiAdapter struct {
	ip       string
	port     string
	username string
	password string

	connected     bool
	chanActions   chan M
	chanResponses chan M
	chanEvents    chan M
	chanErr       chan error
	mutex         *sync.RWMutex
}

func newAMIAdapter(ip string, port string) (*amiAdapter, error) {
	var a = new(amiAdapter)
	a.ip = ip
	a.port = port
	a.mutex = &sync.RWMutex{}

	conn, err := a.openConnection()
	if err != nil {
		return nil, err
	}

	chanOutStreamReader := make(chan byte)
	a.chanActions = make(chan M)

	chanErrStreamReader := streamReader(conn, chanOutStreamReader)
	a.chanErr = chanErrStreamReader
	chanQuitActionWriter := actionWriter(conn, a.chanActions, a.chanErr)
	chanOutStreamParser := streamParser(chanOutStreamReader)
	a.chanResponses, a.chanEvents = classifier(chanOutStreamParser)

	go func() {
		for {
			err := <-chanErrStreamReader
			chanQuitActionWriter <- true
			a.mutex.Lock()
			a.connected = false
			a.mutex.Unlock()

			log.Warn("AMI TCP ERROR", err.Error())
			conn.Close()

			for {
				log.Info("Try reconnect in 1 second")
				time.Sleep(time.Second * 1)

				conn, err = a.openConnection()
				if err != nil {
					log.Warn("AMI Reconnect failed!")
				} else {
					log.Info("AMI Connected")
					chanErrStreamReader = streamReader(conn, chanOutStreamReader)
					a.chanErr = chanErrStreamReader
					chanQuitActionWriter = actionWriter(conn, a.chanActions, a.chanErr)

					_, err = a.Login(a.username, a.password)
					if err != nil {
						log.Error("AMI Login failed!")
					}
					break
				}
			}
		}
	}()

	return a, nil
}

func (a *amiAdapter) Connected() bool {
	return a.connected
}

func (a *amiAdapter) Login(username string, password string) (chan M, error) {
	a.username = username
	a.password = password

	var action = M{
		"Action":   "Login",
		"Username": a.username,
		"Secret":   a.password,
	}

	var result = a.Exec(action)

	if result["Response"] != "Success" {
		return nil, errors.New("Login failed: " + result["Message"])
	}
	a.mutex.Lock()
	defer a.mutex.Unlock()
	a.connected = true

	return a.chanEvents, nil
}

func (a *amiAdapter) Exec(action M) M {
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

func actionWriter(conn *net.TCPConn, in chan M, chanErr chan error) (chanQuit chan bool) {
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

func streamParser(in chan byte) (chanOut chan M) {
	chanOut = make(chan M)

	var data = make(M)
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
							data = make(M)
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

func classifier(in chan M) (chanOutResponses chan M, chanOutEvents chan M) {
	chanOutResponses = make(chan M)
	chanOutEvents = make(chan M)

	go func() {
		for {
			data := <-in

			if _, ok := data["Event"]; ok {
				chanOutEvents <- data
				continue
			}
			if _, ok := data["Response"]; ok {
				chanOutResponses <- data
			}
		}
	}()

	return chanOutResponses, chanOutEvents
}

func (a *amiAdapter) openConnection() (*net.TCPConn, error) {
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

func serialize(data M) []byte {
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
