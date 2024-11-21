package controllers

import (
	"context"
	"errors"
	"log"
	"time"

	"github.com/muka/go-bluetooth/api"
	"github.com/muka/go-bluetooth/bluez/profile/adapter"
	"github.com/muka/go-bluetooth/bluez/profile/agent"
	"github.com/muka/go-bluetooth/bluez/profile/device"
)

const (
	bluetoothRefreshTimeout = time.Second * 5
)

// BluetoothDevice represents a Bluetooth device
type BluetoothDevice struct {
	Address string
	Name    string

	btDevice *device.Device1
}

// Connected returns if this Bluetooth device is connected or not
func (bt *BluetoothDevice) Connected() bool {
	if bt.btDevice == nil {
		return false
	}
	return bt.btDevice.Properties.Connected
}

// BluetoothSetting is a controller to interface with the Bluetooth subsystem.
type BluetoothSetting struct {
	adapter   *adapter.Adapter1
	adapterID string

	devices []BluetoothDevice
}

// NewBluetoothSetting returns a new BluetoothSetting controller for use.
func NewBluetoothSetting(adapter *adapter.Adapter1, adapterID string) *BluetoothSetting {
	return &BluetoothSetting{
		adapter:   adapter,
		adapterID: adapterID,
		devices:   []BluetoothDevice{},
	}
}

// GetDevices returns the list of available Bluetooth devices. This list is cached.
func (bs *BluetoothSetting) GetDevices() []BluetoothDevice {
	return bs.devices
}

// ConnectDevice requests that the specified Bluetooth device be connected.
func (bs *BluetoothSetting) ConnectDevice(dev BluetoothDevice) error {
	if dev.Connected() {
		return nil
	}

	if dev.btDevice == nil {
		return errors.New("device reference missing")
	} else if dev.btDevice.Properties == nil {
		return errors.New("device properties reference missing")
	}

	if !dev.btDevice.Properties.Paired || !dev.btDevice.Properties.Trusted {
		err := dev.btDevice.Pair()
		if err != nil {
			log.Printf("error pairing %s: %s\n", dev.Address, err.Error())
			return err
		}

		agent.SetTrusted(bs.adapterID, dev.btDevice.Path())
	}

	err := dev.btDevice.Connect()
	if err != nil {
		log.Printf("error connecting to %s: %s\n", dev.Address, err.Error())
		return err
	}

	return nil
}

// RefreshDevices refreshes the list of Bluetooth devices. This will run at most 5 seconds
func (bs *BluetoothSetting) RefreshDevices(ctx context.Context) error {
	// Reset the device list
	bs.devices = nil

	devices, err := bs.adapter.GetDevices()
	if err != nil {
		log.Printf("error getting list of devices from adapter: %s\n", err.Error())
		return err
	}

	for _, d := range devices {
		if d == nil || d.Properties == nil {
			continue
		}

		bs.devices = append(bs.devices, BluetoothDevice{
			Address:  d.Properties.Address,
			Name:     d.Properties.Name,
			btDevice: d,
		})
	}

	discovery, discoverCancel, err := api.Discover(bs.adapter, nil)
	if err != nil {
		log.Printf("error starting to discover bluetooth devices: %s\n", err.Error())
		return err
	}
	defer discoverCancel()

	refreshCtx, timeoutCancel := context.WithTimeout(ctx, bluetoothRefreshTimeout)
	defer timeoutCancel()

	for {
		select {
		case discoveredDevice := <-discovery:
			d, err := device.NewDevice1(discoveredDevice.Path)
			if err != nil {
				log.Printf("error creating device from discovered path %s\n", err.Error())
				return err
			}

			if d == nil || d.Properties == nil {
				continue
			}

			bs.devices = append(bs.devices, BluetoothDevice{
				Address:  d.Properties.Address,
				Name:     d.Properties.Name,
				btDevice: d,
			})

		case <-refreshCtx.Done():
			return nil
		}
	}
}
