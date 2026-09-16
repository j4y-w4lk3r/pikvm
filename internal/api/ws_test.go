package api

import "testing"

func TestSwitchChangedDedup(t *testing.T) {
	resetWSDedup()
	t.Cleanup(resetWSDedup)

	s := SwitchState{
		Extenders:   1,
		PortsPerExt: 4,
		TotalPorts:  4,
		ActivePort:  0,
		PowerLeds:   []bool{false, false, false, false},
	}
	if !switchChanged(s) {
		t.Fatal("first switch event should be forwarded")
	}
	if switchChanged(s) {
		t.Fatal("identical switch event should be dropped")
	}
	s.ActivePort = 2
	if !switchChanged(s) {
		t.Fatal("active-port change should be forwarded")
	}
}

func TestAtxAndClientsDedup(t *testing.T) {
	resetWSDedup()
	t.Cleanup(resetWSDedup)

	atx := AtxMsg{PowerLed: true}
	if !atxChanged(atx) {
		t.Fatal("first atx event should be forwarded")
	}
	if atxChanged(atx) {
		t.Fatal("identical atx event should be dropped")
	}
	atx.Busy = true
	if !atxChanged(atx) {
		t.Fatal("atx busy change should be forwarded")
	}

	if !clientsChanged(2) {
		t.Fatal("first clients event should be forwarded")
	}
	if clientsChanged(2) {
		t.Fatal("identical clients count should be dropped")
	}
	if !clientsChanged(3) {
		t.Fatal("clients count change should be forwarded")
	}
}

func TestMsdChangedDuringUpload(t *testing.T) {
	resetWSDedup()
	t.Cleanup(resetWSDedup)

	idle := MsdMsg{Online: true, FreeBytes: 100}
	if !msdChanged(idle) {
		t.Fatal("first msd event should be forwarded")
	}
	if msdChanged(idle) {
		t.Fatal("identical msd event should be dropped")
	}
	uploading := idle
	uploading.Uploading = true
	uploading.UploadName = "netboot.iso"
	uploading.UploadPct = 10
	if !msdChanged(uploading) {
		t.Fatal("upload start should be forwarded")
	}
	uploading.UploadPct = 50
	if !msdChanged(uploading) {
		t.Fatal("upload progress should be forwarded")
	}
}

func TestDispatchEventIgnoresNonJSON(t *testing.T) {
	resetWSDedup()
	t.Cleanup(resetWSDedup)
	// JPEG SOI — must not panic even with a nil program.
	dispatchEvent(nil, []byte{0xff, 0xd8, 0xff})
	dispatchEvent(nil, nil)
}

func TestInfoChangedDedup(t *testing.T) {
	resetInfoDedup()
	t.Cleanup(resetInfoDedup)

	s := InfoState{Platform: "v4", CPUPercent: 12}
	if !infoChanged(s) {
		t.Fatal("first info snapshot should be forwarded")
	}
	if infoChanged(s) {
		t.Fatal("identical info snapshot should be dropped")
	}
	s.CPUPercent = 40
	if !infoChanged(s) {
		t.Fatal("cpu change should be forwarded")
	}
}

func TestResetLiveDedupClearsSnapshots(t *testing.T) {
	_ = switchChanged(SwitchState{TotalPorts: 4, PortsPerExt: 4, Extenders: 1})
	_ = infoChanged(InfoState{Platform: "v3"})
	ResetLiveDedup()
	if !switchChanged(SwitchState{TotalPorts: 4, PortsPerExt: 4, Extenders: 1}) {
		t.Fatal("after reset, the same switch event must be forwarded again")
	}
	if !infoChanged(InfoState{Platform: "v3"}) {
		t.Fatal("after reset, the same info snapshot must be forwarded again")
	}
}
