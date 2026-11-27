package dataplane

import (
	"bytes"
	"crypto/rand"
	_ "embed"
	"encoding/binary"
	"errors"
	"io"
	"net"

	"github.com/cilium/ebpf"
	"github.com/cilium/ebpf/link"
	"golang.org/x/sys/unix"
)

//go:generate clang -O2 -g -fdebug-compilation-dir=. -target bpfel -c bpf/peer_ingress.c -o bpf/peer_ingress.elf
//go:embed bpf/peer_ingress.elf
var linuxPeerIngress []byte

//go:generate sh -c "clang -O2 -g -fdebug-compilation-dir=. -I/usr/include/${DOLLAR}(cc -dumpmachine) -target bpfel -c bpf/peer_output.c -o bpf/peer_output.elf"
//go:embed bpf/peer_output.elf
var linuxPeerOutputELF []byte

func attachLinuxPeerIngress(index int, padding, localPort, kernelPort uint16) (link.Link, error) {
	spec, err := ebpf.LoadCollectionSpecFromReader(bytes.NewReader(linuxPeerIngress))
	if err != nil {
		return nil, err
	}
	values := map[string]uint32{
		"outer_offset": 20 + 8 + 16 + uint32(padding),
		"local_port":   uint32(localPort),
		"kernel_port":  uint32(kernelPort),
	}
	coll, err := loadPeerFilter(spec, values)
	if err != nil {
		return nil, err
	}
	defer coll.Close()
	attached, err := link.AttachXDP(link.XDPOptions{Program: coll.Programs["peer_wire_size"], Interface: index, Flags: link.XDPGenericMode})
	if err != nil {
		return nil, linuxFilterError(err)
	}
	return attached, nil
}

func linuxFilterError(err error) error {
	if errors.Is(err, unix.EPERM) || errors.Is(err, unix.EOPNOTSUPP) || errors.Is(err, unix.ENOSYS) || errors.Is(err, ebpf.ErrNotSupported) {
		return errKernelUnavailable
	}
	return err
}

func loadPeerFilter(spec *ebpf.CollectionSpec, values map[string]uint32) (*ebpf.Collection, error) {
	for name, value := range values {
		variable := spec.Variables[name]
		if variable == nil {
			return nil, ErrEngine
		}
		if err := variable.Set(value); err != nil {
			return nil, err
		}
	}
	coll, err := ebpf.NewCollection(spec)
	if err != nil {
		return nil, linuxFilterError(err)
	}
	return coll, nil
}

func (d *linuxPeerDevice) attachOutput(index int, localPort, kernelPort uint16) error {
	lo, err := net.InterfaceByName("lo")
	if err != nil {
		return err
	}
	spec, err := ebpf.LoadCollectionSpecFromReader(bytes.NewReader(linuxPeerOutputELF))
	if err != nil {
		return err
	}
	var seed [4]byte
	if _, err := io.ReadFull(rand.Reader, seed[:]); err != nil {
		return err
	}
	coll, err := loadPeerFilter(spec, map[string]uint32{"frame_tag": binary.NativeEndian.Uint32(seed[:]) | 1, "source_port": uint32(kernelPort), "destination_port": uint32(localPort)})
	if err != nil {
		return err
	}
	defer coll.Close()
	for _, a := range []struct {
		index   int
		program string
	}{{index, "mark_injection"}, {lo.Index, "classify_local"}} {
		l, err := link.AttachTCX(link.TCXOptions{Interface: a.index, Program: coll.Programs[a.program], Attach: ebpf.AttachTCXEgress})
		if err != nil {
			return linuxFilterError(err)
		}
		d.outputFilters = append(d.outputFilters, l)
	}
	socket, err := d.wire.SyscallConn()
	if err != nil {
		return err
	}
	var sockErr error
	if err := socket.Control(func(fd uintptr) { sockErr = unix.SetsockoptInt(int(fd), unix.IPPROTO_IP, unix.IP_RECVTOS, 1) }); err != nil {
		return err
	}
	return sockErr
}
