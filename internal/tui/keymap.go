package tui

import (
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/lipgloss"
)

// newForm builds a huh form with the MooFetch theme and Spanish key help.
func newForm(groups ...*huh.Group) *huh.Form {
	return huh.NewForm(groups...).WithTheme(newTheme()).WithKeyMap(spanishKeyMap())
}

// newTheme is the Charm theme with the key hints drawn as badges (so it is
// obvious what each key does) and, on consoles that cannot be trusted with wide
// glyphs, plain ASCII markers.
func newTheme() *huh.Theme {
	t := huh.ThemeCharm()
	t.Help.ShortKey = keyChip
	t.Help.FullKey = keyChip
	if currentMode().ASCII {
		t.Focused.SelectedPrefix = lipgloss.NewStyle().SetString("[x] ").Foreground(lipgloss.Color("#02BF87"))
		t.Focused.UnselectedPrefix = lipgloss.NewStyle().SetString("[ ] ")
		t.Blurred.SelectedPrefix = lipgloss.NewStyle().SetString("[x] ")
		t.Blurred.UnselectedPrefix = lipgloss.NewStyle().SetString("[ ] ")
	}
	return t
}

// spanishKeyMap returns huh's default key map with the contextual help shown
// at the bottom of every form translated to Spanish. Keys are unchanged, except
// that "s" also answers "yes" on confirm questions.
func spanishKeyMap() *huh.KeyMap {
	km := huh.NewDefaultKeyMap()

	help := func(b *key.Binding, keys, desc string) { b.SetHelp(keys, desc) }

	in := &km.Input
	help(&in.AcceptSuggestion, "ctrl+e", "completar")
	help(&in.Prev, "shift+tab", "atrás")
	help(&in.Next, "enter", "siguiente")
	help(&in.Submit, "enter", "enviar")

	tx := &km.Text
	help(&tx.Prev, "shift+tab", "atrás")
	help(&tx.Next, "enter", "siguiente")
	help(&tx.Submit, "enter", "enviar")
	help(&tx.NewLine, "alt+enter / ctrl+j", "nueva línea")
	help(&tx.Editor, "ctrl+e", "abrir editor")

	sel := &km.Select
	help(&sel.Prev, "shift+tab", "atrás")
	help(&sel.Next, "enter", "elegir")
	help(&sel.Submit, "enter", "enviar")
	help(&sel.Up, "↑", "arriba")
	help(&sel.Down, "↓", "abajo")
	help(&sel.Left, "←", "izquierda")
	help(&sel.Right, "→", "derecha")
	help(&sel.Filter, "/", "filtrar")
	help(&sel.SetFilter, "esc", "aplicar filtro")
	help(&sel.ClearFilter, "esc", "borrar filtro")
	help(&sel.HalfPageUp, "ctrl+u", "media página arriba")
	help(&sel.HalfPageDown, "ctrl+d", "media página abajo")
	help(&sel.GotoTop, "g/inicio", "ir al inicio")
	help(&sel.GotoBottom, "G/fin", "ir al final")

	ms := &km.MultiSelect
	help(&ms.Prev, "shift+tab", "atrás")
	help(&ms.Next, "enter", "continuar")
	help(&ms.Submit, "enter", "enviar")
	help(&ms.Toggle, "espacio", "marcar/desmarcar")
	help(&ms.Up, "↑", "arriba")
	help(&ms.Down, "↓", "abajo")
	help(&ms.Filter, "/", "filtrar")
	help(&ms.SetFilter, "esc", "aplicar filtro")
	help(&ms.ClearFilter, "esc", "borrar filtro")
	help(&ms.HalfPageUp, "ctrl+u", "media página arriba")
	help(&ms.HalfPageDown, "ctrl+d", "media página abajo")
	help(&ms.GotoTop, "g/inicio", "ir al inicio")
	help(&ms.GotoBottom, "G/fin", "ir al final")
	help(&ms.SelectAll, "ctrl+a", "marcar todos")
	help(&ms.SelectNone, "ctrl+a", "desmarcar todos")

	nt := &km.Note
	help(&nt.Prev, "shift+tab", "atrás")
	help(&nt.Next, "enter", "siguiente")
	help(&nt.Submit, "enter", "enviar")

	cf := &km.Confirm
	help(&cf.Prev, "shift+tab", "atrás")
	help(&cf.Next, "enter", "siguiente")
	help(&cf.Submit, "enter", "enviar")
	help(&cf.Toggle, "←/→", "alternar")
	cf.Accept = key.NewBinding(key.WithKeys("y", "Y", "s", "S"), key.WithHelp("s", "sí"))
	help(&cf.Reject, "n", "no")

	return km
}
