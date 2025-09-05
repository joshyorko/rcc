package remotree

import (
	"net/http"
	"path/filepath"

	"github.com/robocorp/rcc/common"
	"github.com/robocorp/rcc/operations"
	"github.com/robocorp/rcc/pretty"
)

func makeTriggerHandler(requests chan string) http.HandlerFunc {
	return func(response http.ResponseWriter, request *http.Request) {
		catalog := filepath.Base(request.URL.Path)
		defer common.Stopwatch("Trigger of catalog %q took", catalog).Debug()
		requests <- catalog
	}
}

func pullOperation(counter int, catalog, remoteOrigin string) {
	defer common.Stopwatch("#%d: pull opearation lasted", counter).Report()
	common.Log("#%d: Trying to pull %q from %q ...", counter, catalog, remoteOrigin)
	err := operations.PullCatalog(remoteOrigin, catalog, true)
	if err != nil {
		pretty.Warning("#%d: Failed to pull %q from %q, reason: %v", counter, catalog, remoteOrigin, err)
	} else {
		common.Log("#%d: Pull %q from %q completed.", counter, catalog, remoteOrigin)
	}
}

func pullProcess(requests chan string) {
	remoteOrigin := common.RccRemoteOrigin()
	disabled := len(remoteOrigin) == 0
	if disabled {
		pretty.Note("Wont pull anything since RCC_REMOTE_ORIGIN is not defined.")
	}
	counter := 0
forever:
	for {
		catalog, ok := <-requests
		if !ok {
			break forever
		}
		counter += 1
		if disabled {
			pretty.Warning("Cannot #%d pull %q since RCC_REMOTE_ORIGIN is not defined.", counter, catalog)
			continue
		}
		pullOperation(counter, catalog, remoteOrigin)
	}
}
