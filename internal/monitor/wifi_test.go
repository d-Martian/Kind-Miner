package monitor

import "testing"

func TestPowerSaveEnabled(t *testing.T) {
	cases := []struct {
		name    string
		setting uint32
		want    bool
	}{
		{
			name:    "explicitly disabled is the only setting that means off",
			setting: nmPowerSaveDisable,
			want:    false,
		},
		{
			name:    "explicitly enabled",
			setting: nmPowerSaveEnable,
			want:    true,
		},
		{
			// The configuration that actually caused 14-second stalls.
			name:    "default defers to the driver, which enables power save",
			setting: nmPowerSaveDefault,
			want:    true,
		},
		{
			name:    "ignore also leaves the driver default in place",
			setting: nmPowerSaveIgnore,
			want:    true,
		},
		{
			name:    "an unknown future value is not assumed to be safe",
			setting: 99,
			want:    true,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := powerSaveEnabled(c.setting); got != c.want {
				t.Errorf("powerSaveEnabled(%d) = %v, want %v", c.setting, got, c.want)
			}
		})
	}
}
