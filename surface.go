package deskpad

import "image"

// Surface renders the active screen state to a control surface.
type Surface interface {
	ID() string
	KeyCount() int
	Refresh(Snapshot) error
	UpdateKey(Snapshot, int) error
	Clear() error
}

// Snapshot contains the currently rendered control-surface state.
type Snapshot struct {
	ScreenName string
	Rows       int
	Columns    int
	Keys       []image.Image
}

func cloneSnapshot(snapshot Snapshot) Snapshot {
	keys := make([]image.Image, len(snapshot.Keys))
	copy(keys, snapshot.Keys)
	snapshot.Keys = keys
	return snapshot
}
