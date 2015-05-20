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
// amigo.M is a simple alias on map[string]string
func DeviceStateChangeHandler (m amigo.M) {
    logger.Debug("DeviceStateChange event received", m)
}

func DefaultHandler (m amigo.M) {
    logger.Debug("Event received", m)
}


func main() {
    
    // Connect to Asterisk. Required arguments is username and password. 
    // Default host is "127.0.0.1", default port is "5038".
    a := amigo.New("username", "password", "host", "port")
    a.Connect()
    
    
    // Registering handler function for event "DeviceStateChange"
    a.RegisterHandler("DeviceStateChange", DeviceStateChangeHandler)
    
    // Registering default handler function for all events. 
    a.RegisterDefaultHandler(DefaultHandler)
    
    // Optionally create channel to receiving all events
    // and set created channel to receive all events
    c := make(chan map[string]string, 100)
    a.SetEventChannel(c)
    
    
    // Check if connected with Asterisk, will send Action "QueueSummary"
    if a.Connected() {
        result, err := a.Action(amigo.M{"Action": "QueueSummary", "ActionID": "Init"})
        // If not error, processing result. Response on Action will follow in defined events.
        // You need to catch them in event channel, DefaultHandler or specified HandlerFunction
    }
}
```
