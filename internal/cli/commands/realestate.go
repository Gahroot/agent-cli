package commands

import (
	"github.com/spf13/cobra"
	"github.com/unstablemind/pocket/internal/realestate/dotloop"
	"github.com/unstablemind/pocket/internal/realestate/followupboss"
)

func NewRealEstateCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "realestate",
		Aliases: []string{"re", "estate"},
		Short:   "Real estate commands",
		Long:    "Real estate tools: Follow Up Boss CRM, DotLoop transaction management.",
	}

	cmd.AddCommand(followupboss.NewCmd())
	cmd.AddCommand(dotloop.NewCmd())

	return cmd
}
