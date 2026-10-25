package main

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// Flag groups shown in --help. Flags without a group fall under groupOther,
// so subcommands added later degrade gracefully.
const (
	groupInput    = "Entrada"
	groupDownload = "Descarga"
	groupSession  = "Sesión"
	groupOutput   = "Salida"
	groupOther    = "Otros"

	groupAnnotation     = "moofetch_group"
	footerAnnotation    = "moofetch_footer"
	localOnlyAnnotation = "moofetch_local_flags_only"
)

var (
	// groupOrder is the order in which groups are printed.
	groupOrder = []string{groupInput, groupDownload, groupSession, groupOutput, groupOther}

	// flagOrder fixes the order of known flags inside a group; unknown flags
	// follow alphabetically.
	flagOrder = []string{
		"urls", "file",
		"output", "concurrency",
		"auth", "cookie", "uni", "domain",
		"headless", "log",
		"demo", "help", "version",
	}

	// flagMeta names the value of each flag in the help (-o, --output DIR).
	flagMeta = map[string]string{
		"urls": "URLS", "file": "ARCHIVO",
		"output": "DIR", "concurrency": "N",
		"auth": "MODO", "cookie": "COOKIE", "uni": "CLAVE", "domain": "HOST",
		"log": "ARCHIVO",
	}
)

const usageTemplate = `Uso:{{if .Runnable}}
  {{.UseLine}}{{end}}{{if .HasAvailableSubCommands}}
  {{.CommandPath}} <comando> [flags]{{end}}{{if .HasAvailableSubCommands}}

Comandos:{{range .Commands}}{{if (or .IsAvailableCommand (eq .Name "help"))}}
  {{rpad .Name .NamePadding}} {{.Short}}{{end}}{{end}}{{end}}{{with groupedFlags .}}

{{.}}{{end}}{{if .HasExample}}

Ejemplos:
{{.Example}}{{end}}{{with usageFooter .}}

{{.}}{{end}}{{if .HasAvailableSubCommands}}

Usa "{{.CommandPath}} <comando> --help" para ver la ayuda de un comando.{{end}}
`

const exitCodesFooter = `Códigos de salida:
  0     todas las descargas terminaron bien
  1     error general (red, disco u otro fallo en el lote)
  2     uso incorrecto (flag desconocido, falta la cookie o no hay URLs)
  77    sesión inválida o expirada (renueva la cookie)
  130   interrumpido por el usuario (Ctrl+C) o SIGTERM`

func init() {
	cobra.AddTemplateFunc("groupedFlags", groupedFlags)
	cobra.AddTemplateFunc("usageFooter", usageFooter)
}

// setFlagGroup assigns the named flags of fs to a help group.
func setFlagGroup(fs *pflag.FlagSet, group string, names ...string) {
	for _, n := range names {
		f := fs.Lookup(n)
		if f == nil {
			panic("setFlagGroup: unknown flag " + n)
		}
		if f.Annotations == nil {
			f.Annotations = map[string][]string{}
		}
		f.Annotations[groupAnnotation] = []string{group}
	}
}

// configureHelp installs the Spanish usage template, help/version flag texts
// and the help command on the root command.
func configureHelp(root *cobra.Command) {
	root.SetUsageTemplate(usageTemplate)
	root.SetHelpCommand(&cobra.Command{
		Use:   "help [comando]",
		Short: "Muestra la ayuda de un comando",
		Run: func(c *cobra.Command, args []string) {
			target, _, err := c.Root().Find(args)
			if target == nil || err != nil {
				c.Printf("Comando desconocido %q. Usa \"%s --help\" para ver los comandos.\n", strings.Join(args, " "), c.Root().Name())
				return
			}
			target.InitDefaultHelpFlag()
			_ = target.Help()
		},
	})
	root.Annotations = map[string]string{footerAnnotation: exitCodesFooter}

	root.InitDefaultHelpFlag()
	root.InitDefaultVersionFlag()
	root.Flags().Lookup("help").Usage = "muestra esta ayuda"
	root.Flags().Lookup("version").Usage = "muestra la versión"
	setFlagGroup(root.Flags(), groupOther, "help", "version")
}

// configureSubcommandHelp translates the automatic -h flag of a subcommand.
func configureSubcommandHelp(c *cobra.Command) {
	c.InitDefaultHelpFlag()
	c.Flags().Lookup("help").Usage = "muestra esta ayuda"
	setFlagGroup(c.Flags(), groupOther, "help")
}

func usageFooter(c *cobra.Command) string {
	return c.Annotations[footerAnnotation]
}

// groupedFlags renders the local and inherited flags of c split in groups.
// Hidden flags are skipped.
func groupedFlags(c *cobra.Command) string {
	all := pflag.NewFlagSet("all", pflag.ContinueOnError)
	all.AddFlagSet(c.LocalFlags())
	if c.Annotations[localOnlyAnnotation] == "" {
		all.AddFlagSet(c.InheritedFlags())
	}

	byGroup := map[string][]*pflag.Flag{}
	all.VisitAll(func(f *pflag.Flag) {
		if f.Hidden {
			return
		}
		g := groupOther
		if a := f.Annotations[groupAnnotation]; len(a) == 1 {
			g = a[0]
		}
		byGroup[g] = append(byGroup[g], f)
	})

	// Align every description in a single column across groups.
	type line struct{ left, right string }
	width := 0
	rendered := map[string][]line{}
	for g, flags := range byGroup {
		sortFlags(flags)
		for _, f := range flags {
			l := line{left: flagLeft(f), right: flagRight(f)}
			if len(l.left) > width {
				width = len(l.left)
			}
			rendered[g] = append(rendered[g], l)
		}
	}

	groups := append([]string(nil), groupOrder...)
	var extra []string
	for g := range byGroup {
		if !contains(groupOrder, g) {
			extra = append(extra, g)
		}
	}
	sort.Strings(extra)
	groups = append(groups, extra...)

	var sections []string
	for _, g := range groups {
		lines := rendered[g]
		if len(lines) == 0 {
			continue
		}
		var b strings.Builder
		b.WriteString(g + ":")
		for _, l := range lines {
			fmt.Fprintf(&b, "\n  %-*s  %s", width, l.left, l.right)
		}
		sections = append(sections, b.String())
	}
	return strings.Join(sections, "\n\n")
}

func sortFlags(flags []*pflag.Flag) {
	rank := func(name string) int {
		for i, n := range flagOrder {
			if n == name {
				return i
			}
		}
		return len(flagOrder)
	}
	sort.SliceStable(flags, func(i, j int) bool {
		ri, rj := rank(flags[i].Name), rank(flags[j].Name)
		if ri != rj {
			return ri < rj
		}
		return flags[i].Name < flags[j].Name
	})
}

func flagLeft(f *pflag.Flag) string {
	name, _ := pflag.UnquoteUsage(f)
	if m, ok := flagMeta[f.Name]; ok {
		name = m
	}
	s := "    --" + f.Name
	if f.Shorthand != "" {
		s = "-" + f.Shorthand + ", --" + f.Name
	}
	if name != "" {
		s += " " + name
	}
	return s
}

func flagRight(f *pflag.Flag) string {
	_, usage := pflag.UnquoteUsage(f)
	switch f.Value.Type() {
	case "string":
		if f.DefValue != "" {
			usage += " (por defecto " + strconv.Quote(f.DefValue) + ")"
		}
	case "bool":
		// A false default is implicit for switches.
	default:
		if f.DefValue != "" && f.DefValue != "0" {
			usage += " (por defecto " + f.DefValue + ")"
		}
	}
	return usage
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}
