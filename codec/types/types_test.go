package types_test

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/gogo/protobuf/grpc"
	"github.com/gogo/protobuf/proto"
	grpc2 "google.golang.org/grpc"

	"github.com/cosmos/cosmos-sdk/codec"

	"github.com/gogo/protobuf/jsonpb"

	"github.com/cosmos/cosmos-sdk/codec/types"

	"github.com/stretchr/testify/require"

	"github.com/cosmos/cosmos-sdk/testutil/testdata"
)

func TestPackUnpack(t *testing.T) {
	registry := testdata.NewTestInterfaceRegistry()

	spot := &testdata.Dog{Name: "Spot"}
	any := types.Any{}
	err := any.Pack(spot)
	require.NoError(t, err)

	require.Equal(t, spot, any.GetCachedValue())

	// without cache
	any.ClearCachedValue()
	var animal testdata.Animal
	err = registry.UnpackAny(&any, &animal)
	require.NoError(t, err)
	require.Equal(t, spot, animal)

	// with cache
	err = any.Pack(spot)
	require.Equal(t, spot, any.GetCachedValue())
	require.NoError(t, err)
	err = registry.UnpackAny(&any, &animal)
	require.NoError(t, err)
	require.Equal(t, spot, animal)
}

type TestI interface {
	DoSomething()
}

func TestRegister(t *testing.T) {
	registry := types.NewInterfaceRegistry()
	registry.RegisterInterface("Animal", (*testdata.Animal)(nil))
	registry.RegisterInterface("TestI", (*TestI)(nil))
	require.NotPanics(t, func() {
		registry.RegisterImplementations((*testdata.Animal)(nil), &testdata.Dog{})
	})
	require.Panics(t, func() {
		registry.RegisterImplementations((*TestI)(nil), &testdata.Dog{})
	})
	require.Panics(t, func() {
		registry.RegisterImplementations((*TestI)(nil), nil)
	})
	require.Panics(t, func() {
		registry.RegisterInterface("not_an_interface", (*testdata.Dog)(nil))
	})
}

func TestUnpackInterfaces(t *testing.T) {
	registry := testdata.NewTestInterfaceRegistry()

	spot := &testdata.Dog{Name: "Spot"}
	any, err := types.NewAnyWithValue(spot)
	require.NoError(t, err)

	hasAny := testdata.HasAnimal{
		Animal: any,
		X:      1,
	}
	bz, err := hasAny.Marshal()
	require.NoError(t, err)

	var hasAny2 testdata.HasAnimal
	err = hasAny2.Unmarshal(bz)
	require.NoError(t, err)

	err = types.UnpackInterfaces(hasAny2, registry)
	require.NoError(t, err)

	require.Equal(t, spot, hasAny2.Animal.GetCachedValue())
}

func TestNested(t *testing.T) {
	registry := testdata.NewTestInterfaceRegistry()

	spot := &testdata.Dog{Name: "Spot"}
	any, err := types.NewAnyWithValue(spot)
	require.NoError(t, err)

	ha := &testdata.HasAnimal{Animal: any}
	any2, err := types.NewAnyWithValue(ha)
	require.NoError(t, err)

	hha := &testdata.HasHasAnimal{HasAnimal: any2}
	any3, err := types.NewAnyWithValue(hha)
	require.NoError(t, err)

	hhha := testdata.HasHasHasAnimal{HasHasAnimal: any3}

	// marshal
	bz, err := hhha.Marshal()
	require.NoError(t, err)

	// unmarshal
	var hhha2 testdata.HasHasHasAnimal
	err = hhha2.Unmarshal(bz)
	require.NoError(t, err)
	err = types.UnpackInterfaces(hhha2, registry)
	require.NoError(t, err)

	require.Equal(t, spot, hhha2.TheHasHasAnimal().TheHasAnimal().TheAnimal())
}

func TestAny_ProtoJSON(t *testing.T) {
	spot := &testdata.Dog{Name: "Spot"}
	any, err := types.NewAnyWithValue(spot)
	require.NoError(t, err)

	jm := &jsonpb.Marshaler{}
	json, err := jm.MarshalToString(any)
	require.NoError(t, err)
	require.Equal(t, "{\"@type\":\"/testdata.Dog\",\"name\":\"Spot\"}", json)

	registry := testdata.NewTestInterfaceRegistry()
	jum := &jsonpb.Unmarshaler{}
	var any2 types.Any
	err = jum.Unmarshal(strings.NewReader(json), &any2)
	require.NoError(t, err)
	var animal testdata.Animal
	err = registry.UnpackAny(&any2, &animal)
	require.NoError(t, err)
	require.Equal(t, spot, animal)

	ha := &testdata.HasAnimal{
		Animal: any,
	}
	err = ha.UnpackInterfaces(types.ProtoJSONPacker{JSONPBMarshaler: jm})
	require.NoError(t, err)
	json, err = jm.MarshalToString(ha)
	require.NoError(t, err)
	require.Equal(t, "{\"animal\":{\"@type\":\"/testdata.Dog\",\"name\":\"Spot\"}}", json)

	require.NoError(t, err)
	var ha2 testdata.HasAnimal
	err = jum.Unmarshal(strings.NewReader(json), &ha2)
	require.NoError(t, err)
	err = ha2.UnpackInterfaces(registry)
	require.NoError(t, err)
	require.Equal(t, spot, ha2.Animal.GetCachedValue())
}

type testAnyPackClient struct {
	any               types.Any
	interfaceRegistry types.InterfaceRegistry
}

var _ grpc.ClientConn = &testAnyPackClient{}

func (t *testAnyPackClient) Invoke(_ context.Context, method string, args, _ interface{}, _ ...grpc2.CallOption) error {
	reqMsg, ok := args.(proto.Message)
	if !ok {
		return fmt.Errorf("can't proto marshal %T", args)
	}

	t.interfaceRegistry.RegisterServiceRequestType((*interface{})(nil), method, reqMsg)

	bz, err := proto.Marshal(reqMsg)
	if err != nil {
		return err
	}

	t.any.TypeUrl = method
	t.any.Value = bz

	return nil
}

func (t *testAnyPackClient) NewStream(context.Context, *grpc2.StreamDesc, string, ...grpc2.CallOption) (grpc2.ClientStream, error) {
	return nil, fmt.Errorf("not supported")
}

func TestAny_ServiceRequestProtoJSON(t *testing.T) {
	interfaceRegistry := types.NewInterfaceRegistry()
	anyPacker := &testAnyPackClient{interfaceRegistry: interfaceRegistry}
	dogMsgClient := testdata.NewMsgClient(anyPacker)
	_, err := dogMsgClient.CreateDog(context.Background(), &testdata.MsgCreateDog{Dog: &testdata.Dog{
		Name: "spot",
	}})
	require.NoError(t, err)

	cdc := codec.NewProtoCodec(interfaceRegistry)
	bz, err := cdc.MarshalJSON(&anyPacker.any)
	require.NoError(t, err)
	require.Equal(t,
		`{"@type":"/testdata.Msg/CreateDog","dog":{"size":"","name":"spot"}}`,
		string(bz))

	var any2 types.Any
	err = cdc.UnmarshalJSON(bz, &any2)
	require.NoError(t, err)
	require.Equal(t, anyPacker.any, any2)
}
