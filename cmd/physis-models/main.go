// Command physis-models prints the device models as a Markdown table, so the
// documents quote exactly what the code computes with.
package main

import (
	"fmt"
	"strconv"

	"github.com/kuberoot-dev/physis/pkg/device"
)

func g(v float64) string {
	if v == 0 {
		return "—"
	}
	return strconv.FormatFloat(v, 'g', 3, 64)
}

func cell(v device.Value) string {
	if v.V == 0 {
		return "—"
	}
	s := g(v.V)
	if v.Lo != 0 || v.Hi != 0 {
		s += fmt.Sprintf(" (%s–%s)", g(v.Lo), g(v.Hi))
	}
	return s + " · " + v.Source
}

func main() {
	fmt.Println("| Физика | Эмуляция | Операций/с | Энергия на операцию, Дж | Мощность, Вт | Загрузка, с | Загрузка, Дж | Калибровка | Ёмкость |")
	fmt.Println("|---|---|---|---|---|---|---|---|---|")
	for _, p := range append(append(append([]device.Physics{}, device.All...), device.Numeric...), device.Hosted...) {
		m := device.Builtin(p)
		cal := "—"
		if m.CalEvery > 0 {
			cal = fmt.Sprintf("%g с каждые %g ч", m.CalDuration, m.CalEvery/3600)
		}
		emu := "нет"
		if m.Emulated {
			emu = "да"
		}
		fmt.Printf("| %s | %s | %s | %s | %s | %s | %s | %s | %d |\n", p, emu, cell(m.OpsPerSecond), cell(m.EnergyPerOp), cell(m.ActivePowerW), cell(m.SetupSeconds), cell(m.SetupJoules), cal, m.MaxSize)
	}
}
