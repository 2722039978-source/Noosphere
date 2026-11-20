package secure

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/godbus/dbus/v5"
)

const (
	secretService = "org.freedesktop.secrets"
	secretRoot    = dbus.ObjectPath("/org/freedesktop/secrets")
	secretPrefix  = "org.freedesktop.Secret."
)

type platformStore struct{}

func (platformStore) Provider() string { return "linux_secret_service" }

type secretValue struct {
	Session     dbus.ObjectPath
	Parameters  []byte
	Value       []byte
	ContentType string
}

func secretAttributes() map[string]string {
	return map[string]string{"application": "noosphere", "credential": credTarget}
}

func (s platformStore) failure(err error) *StoreError {
	var dbusError dbus.Error
	if errors.As(err, &dbusError) {
		switch dbusError.Name {
		case secretPrefix + "Error.IsLocked":
			return storeError(s, Locked)
		case "org.freedesktop.DBus.Error.AccessDenied", "org.freedesktop.DBus.Error.AuthFailed":
			return storeError(s, PermissionDenied)
		}
	}
	return storeError(s, Unavailable)
}

func (s platformStore) connect(ctx context.Context) (*dbus.Conn, string, error) {
	address := os.Getenv("DBUS_SESSION_BUS_ADDRESS")
	if address == "" {
		address = fmt.Sprintf("unix:path=/run/user/%d/bus", os.Getuid())
	}
	// Restrict secrets to the local session bus. Connect must not invoke
	// dbus-launch or fall back to a TCP transport.
	for _, entry := range strings.Split(address, ";") {
		if !strings.HasPrefix(entry, "unix:path=") && !strings.HasPrefix(entry, "unix:abstract=") {
			return nil, "", storeError(s, Unavailable)
		}
	}
	conn, err := dbus.Connect(address, dbus.WithContext(ctx))
	if err != nil {
		return nil, "", s.failure(err)
	}
	var owner string
	err = conn.BusObject().CallWithContext(ctx, "org.freedesktop.DBus.GetNameOwner", 0, secretService).Store(&owner)
	var lookupError dbus.Error
	if errors.As(err, &lookupError) && lookupError.Name == "org.freedesktop.DBus.Error.NameHasNoOwner" {
		err = conn.BusObject().CallWithContext(ctx, "org.freedesktop.DBus.StartServiceByName", 0, secretService, uint32(0)).Err
		if err == nil {
			err = conn.BusObject().CallWithContext(ctx, "org.freedesktop.DBus.GetNameOwner", 0, secretService).Store(&owner)
		}
	}
	if err != nil {
		conn.Close()
		return nil, "", s.failure(err)
	}
	return conn, owner, nil
}

func (s platformStore) find(ctx context.Context, service dbus.BusObject) (dbus.ObjectPath, error) {
	var unlocked, locked []dbus.ObjectPath
	err := service.CallWithContext(ctx, secretPrefix+"Service.SearchItems", 0, secretAttributes()).Store(&unlocked, &locked)
	if err != nil {
		return "", s.failure(err)
	}
	switch {
	case len(unlocked)+len(locked) > 1:
		return "", storeError(s, Corrupt)
	case len(locked) > 0:
		return "", storeError(s, Locked)
	case len(unlocked) == 0:
		return "", storeError(s, NotFound)
	default:
		return unlocked[0], nil
	}
}

func (s platformStore) session(ctx context.Context, service dbus.BusObject) (dbus.ObjectPath, error) {
	var ignored dbus.Variant
	var session dbus.ObjectPath
	// The Secret Service plain session protocol is confined to authenticated
	// clients on the local Unix session bus and messages to a unique owner.
	err := service.CallWithContext(ctx, secretPrefix+"Service.OpenSession", 0, "plain", dbus.MakeVariant("")).Store(&ignored, &session)
	if err != nil || session == "/" || !session.IsValid() {
		return "", s.failure(err)
	}
	return session, nil
}

func (s platformStore) Load(ctx context.Context) ([]byte, error) {
	conn, owner, err := s.connect(ctx)
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	service := conn.Object(owner, secretRoot)
	path, err := s.find(ctx, service)
	if err != nil {
		return nil, err
	}
	session, err := s.session(ctx, service)
	if err != nil {
		return nil, err
	}
	var value secretValue
	err = conn.Object(owner, path).CallWithContext(ctx, secretPrefix+"Item.GetSecret", 0, session).Store(&value)
	if err != nil {
		return nil, s.failure(err)
	}
	if value.Session != session || len(value.Parameters) != 0 || len(value.Value) != keySize {
		return nil, storeError(s, Corrupt)
	}
	return value.Value, nil
}

func (s platformStore) Create(ctx context.Context, key []byte) error {
	if len(key) != keySize {
		return storeError(s, Corrupt)
	}
	conn, owner, err := s.connect(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()
	service := conn.Object(owner, secretRoot)
	if _, err := s.find(ctx, service); err == nil {
		return storeError(s, alreadyExists)
	} else {
		var failure *StoreError
		if !errors.As(err, &failure) || failure.Status != NotFound {
			return err
		}
	}
	var path dbus.ObjectPath
	err = service.CallWithContext(ctx, secretPrefix+"Service.ReadAlias", 0, "default").Store(&path)
	if err != nil || path == "/" || !path.IsValid() {
		return s.failure(err)
	}
	collection := conn.Object(owner, path)
	var locked dbus.Variant
	err = collection.CallWithContext(ctx, "org.freedesktop.DBus.Properties.Get", 0, secretPrefix+"Collection", "Locked").Store(&locked)
	isLocked, valid := locked.Value().(bool)
	if err != nil || !valid {
		return s.failure(err)
	}
	if isLocked {
		return storeError(s, Locked)
	}
	session, err := s.session(ctx, service)
	if err != nil {
		return err
	}
	properties := map[string]dbus.Variant{
		secretPrefix + "Item.Label":      dbus.MakeVariant("Noosphere Gateway master key"),
		secretPrefix + "Item.Attributes": dbus.MakeVariant(secretAttributes()),
	}
	value := secretValue{Session: session, Value: key, ContentType: "application/octet-stream"}
	var item, prompt dbus.ObjectPath
	err = collection.CallWithContext(ctx, secretPrefix+"Collection.CreateItem", 0, properties, value, false).Store(&item, &prompt)
	if err != nil {
		return s.failure(err)
	}
	if prompt != "/" {
		return storeError(s, Locked)
	}
	if item == "/" || !item.IsValid() {
		return storeError(s, Unavailable)
	}
	return nil
}
