package networkprobe

import "testing"

func TestNormalizeConfigDefaultsToTCP(t *testing.T) {
	config, err := NormalizeConfig(Config{Targets: []Target{
		{Carrier: CarrierTelecom, Host: "127.0.0.1"},
		{Carrier: CarrierUnicom, Host: "127.0.0.2"},
		{Carrier: CarrierMobile, Host: "127.0.0.3"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if config.Probe.Count != 10 || config.Probe.IntervalMs != 200 || config.Probe.TimeoutMs != 1000 {
		t.Fatalf("unexpected probe defaults: %+v", config.Probe)
	}
	for _, target := range config.Targets {
		if target.Protocol != ProtocolTCP || target.Port != 443 || target.AddressFamily != FamilyAuto {
			t.Fatalf("unexpected target defaults: %+v", target)
		}
	}
}

func TestNormalizeConfigRejectsDuplicateCarrier(t *testing.T) {
	_, err := NormalizeConfig(Config{Targets: []Target{
		{Carrier: CarrierTelecom, Host: "127.0.0.1"},
		{Carrier: CarrierTelecom, Host: "127.0.0.2"},
		{Carrier: CarrierMobile, Host: "127.0.0.3"},
	}})
	if err == nil {
		t.Fatal("duplicate carrier was accepted")
	}
}
