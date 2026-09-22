//go:build linux

package client

import (
	"context"
	"errors"
	"testing"

	"golang.org/x/sys/unix"
)

func TestExitLANBPFUAPIConstants(t *testing.T) {
	for _, pair := range [][2]int{{exitLANBPFMapCreate, unix.BPF_MAP_CREATE}, {exitLANBPFMapUpdate, unix.BPF_MAP_UPDATE_ELEM}, {exitLANBPFMapDelete, unix.BPF_MAP_DELETE_ELEM}, {exitLANBPFProgLoad, unix.BPF_PROG_LOAD}, {exitLANBPFMapFreeze, unix.BPF_MAP_FREEZE}, {exitLANBPFArray, unix.BPF_MAP_TYPE_ARRAY}, {exitLANBPFArrayOfMaps, unix.BPF_MAP_TYPE_ARRAY_OF_MAPS}, {exitLANBPFReadOnlyProgram, unix.BPF_F_RDONLY_PROG}, {32, unix.BPF_PROG_TYPE_NETFILTER}, {45, unix.BPF_NETFILTER}} {
		if pair[0] != pair[1] {
			t.Fatal("pinned UAPI mismatch", pair)
		}
	}
}

func TestNativeExitLANBPFPrecancelDoesNotReadOrLoad(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if p, err := newNativeExitLANBPFPreparation(ctx, 7); p != nil || !errors.Is(err, context.Canceled) {
		t.Fatal("cancelled native load attempted", err)
	}
}

func TestExitLANBPFLinkUAPIConstants(t *testing.T) {
	for _, pair := range [][2]int{{exitLANBPFInfo, unix.BPF_OBJ_GET_INFO_BY_FD}, {exitLANBPFLinkCreate, unix.BPF_LINK_CREATE}, {exitLANBPFNetfilterLink, unix.BPF_LINK_TYPE_NETFILTER}, {exitLANBPFLinkDetach, unix.BPF_LINK_DETACH}} {
		if pair[0] != pair[1] {
			t.Fatal("pinned link UAPI mismatch", pair)
		}
	}
}

func TestExitLANBPFPinUAPIConstants(t *testing.T) {
	for _, pair := range [][2]int{{exitLANBPFObjectPin, unix.BPF_OBJ_PIN}, {exitLANBPFObjectGet, unix.BPF_OBJ_GET}, {exitLANBPFPathFD, unix.BPF_F_PATH_FD}} {
		if pair[0] != pair[1] {
			t.Fatal("pinned object UAPI mismatch", pair)
		}
	}
}
