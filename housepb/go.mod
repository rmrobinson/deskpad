// Code in this directory is generated from github.com/rmrobinson/house's
// .proto sources (which that repo builds via Bazel/rules_proto_grpc rather
// than committing generated Go code). deskpad vendors it here and points a
// replace directive at this module so the api/command/device/trait packages
// are importable as github.com/rmrobinson/house/... via plain `go build`.
// Regenerate with scripts/gen-housepb.sh whenever the upstream .proto files
// change.
module github.com/rmrobinson/house

go 1.26.4

require (
	google.golang.org/genproto/googleapis/rpc v0.0.0-20260226221140-a57be14db171
	google.golang.org/grpc v1.81.1
	google.golang.org/protobuf v1.36.11
)

require (
	golang.org/x/net v0.51.0 // indirect
	golang.org/x/sys v0.42.0 // indirect
	golang.org/x/text v0.34.0 // indirect
)
