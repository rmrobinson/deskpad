package deskpad

import (
	"context"
	"image"
)

// Screen is a logically grouped set of keys which are handled together.
type Screen interface {
	Name() string
	Show() []image.Image
	Icon() image.Image
	KeyPressed(ctx context.Context, id int, t KeyPressType) (KeyPressAction, error)
}
