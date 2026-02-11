module fdo-client

go 1.25.0

require github.com/fido-device-onboard/go-fdo v0.0.0-20260211XXXXX

require (
	github.com/fido-device-onboard/go-fdo/fsim v0.0.0-20260116133239-94bd9c5d647c
	gopkg.in/yaml.v3 v3.0.1
)

replace github.com/fido-device-onboard/go-fdo => /var/bkgdata/go-fdo-merge

replace github.com/fido-device-onboard/go-fdo/fsim => /var/bkgdata/go-fdo-merge/fsim
