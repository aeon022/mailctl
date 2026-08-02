package cmd

import (
	"fmt"
	"os"
	"os/exec"

	"github.com/aeon022/mailctl/internal/templates"
	"github.com/spf13/cobra"
)

var templateCmd = &cobra.Command{
	Use:   "template",
	Short: "Manage reusable email snippet templates",
}

var templateListCmd = &cobra.Command{
	Use:   "list",
	Short: "List saved templates",
	RunE: func(cmd *cobra.Command, args []string) error {
		names, err := templates.List()
		if err != nil {
			return err
		}
		if isJSON() {
			outputJSON(names)
			return nil
		}
		if len(names) == 0 {
			fmt.Println("No templates yet — press t in compose, or `mailctl template new <name>`.")
			return nil
		}
		for _, n := range names {
			fmt.Println(n)
		}
		return nil
	},
}

var templateShowCmd = &cobra.Command{
	Use:   "show <name>",
	Short: "Print a template's subject and body",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		d, err := templates.Load(args[0])
		if err != nil {
			return err
		}
		if isJSON() {
			outputJSON(map[string]any{"subject": d.Subject, "body": d.Body})
			return nil
		}
		fmt.Printf("Subject: %s\n\n%s\n", d.Subject, d.Body)
		return nil
	},
}

var templateNewCmd = &cobra.Command{
	Use:   "new <name>",
	Short: "Create a new template and open it in $EDITOR",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		name := args[0]
		path := templates.Dir() + "/" + name + ".md"
		if _, err := os.Stat(path); os.IsNotExist(err) {
			if err := templates.Save(name, "Subject here", "Body here. Use {{.name}} for variables."); err != nil {
				return err
			}
		}
		editor := os.Getenv("EDITOR")
		if editor == "" {
			editor = "vi"
		}
		c := exec.Command(editor, path)
		c.Stdin, c.Stdout, c.Stderr = os.Stdin, os.Stdout, os.Stderr
		return c.Run()
	},
}

var templateDeleteCmd = &cobra.Command{
	Use:   "delete <name>",
	Short: "Delete a template",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := templates.Delete(args[0]); err != nil {
			return err
		}
		fmt.Printf("Deleted template %q\n", args[0])
		return nil
	},
}

func init() {
	templateCmd.AddCommand(templateListCmd, templateShowCmd, templateNewCmd, templateDeleteCmd)
	rootCmd.AddCommand(templateCmd)
}
