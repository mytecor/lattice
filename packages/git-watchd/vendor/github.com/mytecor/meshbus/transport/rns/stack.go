package rns

import (
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
	"sync"

	"github.com/Quad4-Software/Reticulum-Go/pkg/common"
	"github.com/Quad4-Software/Reticulum-Go/pkg/interfaces"
	"github.com/Quad4-Software/Reticulum-Go/pkg/sharedinstance"
	rnstransport "github.com/Quad4-Software/Reticulum-Go/pkg/transport"
)

var ErrSharedInstanceUnavailable = errors.New("RNS shared instance is not running")

type sharedConnector func(*rnstransport.Transport) (*sharedinstance.Instance, error)

// serializedLocalClient compensates for Reticulum-Go v1.2.0's reusable local
// interface transmit buffer, which is not safe when transport packet workers
// send concurrently. It stays at the adapter boundary and can be removed when
// the upstream interface serializes its own writes.
type serializedLocalClient struct {
	*interfaces.LocalClientInterface
	sendMu sync.Mutex
}

func (c *serializedLocalClient) Send(data []byte, address string) error {
	c.sendMu.Lock()
	defer c.sendMu.Unlock()
	return c.LocalClientInterface.Send(data, address)
}

func (c *serializedLocalClient) ProcessOutgoing(data []byte) error {
	c.sendMu.Lock()
	defer c.sendMu.Unlock()
	return c.LocalClientInterface.ProcessOutgoing(data)
}

type stack struct {
	transport  *rnstransport.Transport
	interfaces []interfaces.Interface
	started    []interfaces.Interface
	shared     *sharedinstance.Instance
	required   bool
	connect    sharedConnector
}

func newStack(mode StackMode, config *common.ReticulumConfig, provided ...interfaces.Interface) (*stack, error) {
	switch mode {
	case StackSharedClient:
		if config != nil {
			return nil, fmt.Errorf("%w: shared-client mode does not accept standalone Reticulum configuration", ErrInvalidConfig)
		}
		if len(provided) != 0 {
			return nil, fmt.Errorf("%w: shared-client mode does not accept standalone interfaces", ErrInvalidConfig)
		}
		sharedConfig := common.NewReticulumConfig()
		sharedConfig.EnableTransport = false
		sharedConfig.ShareInstance = true
		sharedConfig.InMemoryStorage = true
		return &stack{
			transport: rnstransport.NewTransport(sharedConfig),
			required:  true,
			connect:   connectRequiredSharedInstance,
		}, nil
	case StackStandalone:
		if config == nil {
			return nil, fmt.Errorf("%w: standalone mode requires Reticulum configuration", ErrInvalidConfig)
		}
	default:
		return nil, fmt.Errorf("%w: unsupported Reticulum stack mode %d", ErrInvalidConfig, mode)
	}

	result := &stack{transport: rnstransport.NewTransport(config)}
	// Caller-supplied interfaces bypass config-driven construction. Tests use
	// this to wrap an interface (packet loss injection, capture) while keeping
	// the transport and identity machinery intact.
	if len(provided) > 0 {
		result.interfaces = append(result.interfaces, provided...)
		return result, nil
	}
	for name, interfaceConfig := range config.Interfaces {
		if !interfaceConfig.Enabled {
			continue
		}
		normalized := normalizeInterfaceConfig(interfaceConfig)
		value, err := interfaces.NewFromConfig(name, normalized)
		if err != nil {
			return nil, fmt.Errorf("construct interface %q: %w", name, err)
		}
		result.interfaces = append(result.interfaces, value)
	}
	if len(result.interfaces) == 0 {
		return nil, fmt.Errorf("%w: at least one enabled Reticulum interface is required", ErrInvalidConfig)
	}
	return result, nil
}

func normalizeInterfaceConfig(config *common.InterfaceConfig) *common.InterfaceConfig {
	copy := *config
	if copy.Type != "UDPInterface" {
		return &copy
	}
	copy.Address = addressWithPort(copy.Address, copy.Port)
	target := copy.TargetAddress
	if target == "" {
		target = copy.TargetHost
	}
	copy.TargetHost = addressWithPort(target, copy.TargetPort)
	return &copy
}

func addressWithPort(address string, port int) string {
	address = strings.TrimSpace(address)
	if address == "" || port == 0 {
		return address
	}
	if _, _, err := net.SplitHostPort(address); err == nil {
		return address
	}
	return net.JoinHostPort(strings.Trim(address, "[]"), strconv.Itoa(port))
}

func (s *stack) Start() error {
	if err := s.transport.Start(); err != nil {
		return err
	}
	if err := s.transport.InitializePathRequestHandler(); err != nil {
		_ = s.transport.Close()
		return err
	}
	if s.required {
		instance, err := s.connect(s.transport)
		if err != nil {
			_ = s.transport.Close()
			return err
		}
		s.shared = instance
		return nil
	}
	for _, value := range s.interfaces {
		if err := value.Start(); err != nil {
			_ = s.Close()
			return fmt.Errorf("start interface %q: %w", value.GetName(), err)
		}
		network, ok := value.(common.NetworkInterface)
		if !ok {
			_ = value.Stop()
			_ = s.Close()
			return fmt.Errorf("interface %q is not a network interface", value.GetName())
		}
		if err := s.transport.RegisterInterface(value.GetName(), network); err != nil {
			_ = value.Stop()
			_ = s.Close()
			return fmt.Errorf("register interface %q: %w", value.GetName(), err)
		}
		s.started = append(s.started, value)
	}
	return nil
}

func (s *stack) Close() error {
	var first error
	if s.shared != nil {
		s.shared.Close()
		s.shared = nil
	}
	for index := len(s.started) - 1; index >= 0; index-- {
		if err := s.started[index].Stop(); err != nil && first == nil {
			first = err
		}
	}
	s.started = nil
	if err := s.transport.Close(); err != nil && first == nil {
		first = err
	}
	return first
}

// connectRequiredSharedInstance attaches only as a client. Unlike
// sharedinstance.Attach, it can never fall back to owning the shared-instance
// listener when no daemon is available.
func connectRequiredSharedInstance(transport *rnstransport.Transport) (*sharedinstance.Instance, error) {
	config := common.NewReticulumConfig()
	useUnix := common.SharedInstanceUsesUnix(config.SharedInstanceType)
	socketPath := config.InstanceName
	if useUnix && socketPath == "" {
		socketPath = "default"
	}
	return connectSharedInstanceAt(transport, config.SharedInstancePort, socketPath, useUnix)
}

func connectSharedInstanceAt(transport *rnstransport.Transport, port int, socketPath string, useUnix bool) (*sharedinstance.Instance, error) {
	client, err := interfaces.NewLocalClientInterface(port, socketPath, useUnix, nil)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrSharedInstanceUnavailable, err)
	}
	client.SetDisconnectHooks(
		func() { transport.SetConnectedToSharedInstance(false) },
		func() { transport.SetConnectedToSharedInstance(true) },
	)
	// Mark the transport before registering the live local interface. Start
	// launches the interface read loop, and RegisterInterface makes inbound
	// packets visible to the transport immediately; setting this afterwards
	// races packet filtering on a busy shared instance.
	transport.SetConnectedToSharedInstance(true)
	if err := client.Start(); err != nil {
		transport.SetConnectedToSharedInstance(false)
		_ = client.Stop()
		return nil, fmt.Errorf("%w: %v", ErrSharedInstanceUnavailable, err)
	}
	if err := transport.RegisterInterface(client.GetName(), &serializedLocalClient{LocalClientInterface: client}); err != nil {
		transport.SetConnectedToSharedInstance(false)
		_ = client.Stop()
		return nil, fmt.Errorf("register RNS shared-instance client: %w", err)
	}
	return &sharedinstance.Instance{Mode: sharedinstance.ModeClient, Client: client}, nil
}
