# amigo
Asterisk AMI connector on golang.

Usage is pretty simple.

To install:
`go get github.com/ivahaev/amigo`

Then import module to your project:
```go
import "github.com/ivahaev/amigo"
```

Then use:
```go
import (
    "github.com/ivahaev/amigo"
)

// Creating hanlder functions
func DeviceStateChangeHandler (m amigo.M) {
    logger.Debug("DeviceStateChange event received", m)
}

func DefaultHandler (m amigo.M) {
    logger.Debug("Event received", m)
}


func main() {
    
    // Connect to Asterisk. Required parameters is username and password. Default host is "127.0.0.1", default port is "5038".
    a := amigo.New("username", "password", "host", "port")
    a.Connect()
    
    // Creating channel to receiving all events
    c := make(chan map[string]string, 100)
    
    // Set created channel to receive all events
    a.SetEventChannel(c)
    
    // Registering handler function for event "DeviceStateChange"
    a.RegisterHandler("DeviceStateChange", DeviceStateChangeHandler)
    
    // Registering default handler function for all events. 
    a.RegisterDefaultHandler(DefaultHandler)
    
    
    // Check if connected with Asterisk, will send Action "QueueSummary"
    if amigo.Connected() {
        result, err := amigo.Action(map[string]string{"Action": "QueueSummary", "ActionID": "Init"})
        // If not error, processing result. Response on Action will follow in defined events.
        // You need to catch them in event channel, DefaultHandler or specified HandlerFunction
    }
}
```
