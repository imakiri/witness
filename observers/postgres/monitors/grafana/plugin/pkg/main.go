package main

import (
	"os"

	"github.com/grafana/grafana-plugin-sdk-go/backend/datasource"
	"github.com/grafana/grafana-plugin-sdk-go/backend/log"

	wplugin "github.com/imakiri/witness/observers/postgres/monitors/grafana/plugin/pkg/plugin"
)

func main() {
	if err := datasource.Manage("imakiri-witness-datasource", wplugin.NewDatasource, datasource.ManageOpts{}); err != nil {
		log.DefaultLogger.Error("plugin exit", "err", err)
		os.Exit(1)
	}
}
