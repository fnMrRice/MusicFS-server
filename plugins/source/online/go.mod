module github.com/fnMrRice/MusicFS-server/plugins/source/online

go 1.26.0

toolchain go1.26.9

require github.com/fnMrRice/MusicFS-server/sdk v0.1.0

require (
	github.com/google/uuid v1.6.0 // indirect
	github.com/santhosh-tekuri/jsonschema/v6 v6.0.3 // indirect
	golang.org/x/text v0.43.0 // indirect
)

replace github.com/fnMrRice/MusicFS-server/sdk => ../../../sdk
