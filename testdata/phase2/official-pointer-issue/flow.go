package fixture

import (
	"log"
	"net"
	"path/filepath"
	"reflect"
	"sync"
	"time"

	"google.golang.org/protobuf/proto"
)

const PluginBuild = "go-plugin v1 (base SDK + detached custom RPC query path)"
const socketPath = "canopy.sock"

type Config struct{ DataDirPath string }
type Contract struct{ fsmId uint64 }
type PluginError struct{}
type FSMToPlugin_Config struct{ Config *PluginConfig }

type Plugin struct {
	pluginConfig    *PluginConfig
	fsmConfig       *PluginConfig
	conn            net.Conn
	pending         map[uint64]chan isFSMToPlugin_Payload
	requestContract map[uint64]*Contract
	l               sync.Mutex
	config          Config
}

func StartPlugin(c Config) *Plugin {
	log.Printf("==== STARTING %s ====", PluginBuild)
	var conn net.Conn
	sockPath := filepath.Join(c.DataDirPath, socketPath)
	for range time.Tick(time.Second) {
		var err error
		conn, err = net.Dial("unix", sockPath)
		if err == nil {
			break
		}
		log.Printf("Failed to connect to plugin socket%v\n", err)
	}
	p := &Plugin{
		pluginConfig:    ContractConfig,
		conn:            conn,
		pending:         map[uint64]chan isFSMToPlugin_Payload{},
		requestContract: map[uint64]*Contract{},
		l:               sync.Mutex{},
		config:          c,
	}
	go p.ListenForInbound()
	if err := p.Handshake(); err != nil {
		log.Fatal(err.Error())
	}
	return p
}

func (p *Plugin) Handshake() *PluginError {
	log.Println("Handshaking with FSM")
	response, err := p.sendToPluginSync(&Contract{}, &PluginToFSM_Config{Config: p.pluginConfig})
	if err != nil {
		return err
	}
	wrapper, ok := response.(*FSMToPlugin_Config)
	if !ok {
		return ErrUnexpectedFSMToPlugin(reflect.TypeOf(response))
	}
	p.fsmConfig = wrapper.Config
	return nil
}

func (p *Plugin) sendToPluginSync(c *Contract, request isPluginToFSM_Payload) (isFSMToPlugin_Payload, *PluginError) {
	ch, requestId, err := p.sendToPluginAsync(c, request)
	if err != nil {
		return nil, err
	}
	response, err := p.waitForResponse(ch, requestId)
	p.l.Lock()
	delete(p.requestContract, requestId)
	p.l.Unlock()
	return response, err
}

func (p *Plugin) sendToPluginAsync(c *Contract, request isPluginToFSM_Payload) (ch chan isFSMToPlugin_Payload, requestId uint64, err *PluginError) {
	requestId = c.fsmId
	ch = make(chan isFSMToPlugin_Payload, 1)
	p.l.Lock()
	p.pending[requestId] = ch
	p.requestContract[requestId] = c
	p.l.Unlock()
	err = p.sendProtoMsg(&PluginToFSM{Id: requestId, Payload: request})
	return
}

func (p *Plugin) sendProtoMsg(ptr proto.Message) *PluginError {
	bz, err := Marshal(ptr)
	if err != nil {
		return err
	}
	return p.sendLengthPrefixed(bz)
}

var marshaller = proto.MarshalOptions{Deterministic: true}

func Marshal(message any) ([]byte, *PluginError) {
	protoBytes, err := marshaller.Marshal(message.(proto.Message))
	if err != nil {
		return nil, ErrMarshal(err)
	}
	return protoBytes, nil
}

func (p *Plugin) ListenForInbound() {}
func (p *Plugin) waitForResponse(chan isFSMToPlugin_Payload, uint64) (isFSMToPlugin_Payload, *PluginError) {
	return nil, nil
}
func (p *Plugin) sendLengthPrefixed([]byte) *PluginError { return nil }
func (e *PluginError) Error() string                     { return "" }
func ErrUnexpectedFSMToPlugin(reflect.Type) *PluginError { return &PluginError{} }
func ErrMarshal(error) *PluginError                      { return &PluginError{} }
