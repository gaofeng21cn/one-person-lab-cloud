package server

import (
	"log"
)

func (app *controlPlaneServer) observeControlledPilotAlerts(metrics map[string]any) {
	active := map[string]bool{}
	alerts, _ := metrics["alerts"].([]any)
	for _, value := range alerts {
		if alert, ok := value.(map[string]any); ok {
			active[stringValue(alert["code"])] = true
		}
	}
	for _, code := range []string{"controlled_pilot_config_invalid", "controlled_pilot_first_failure", "controlled_pilot_capacity_exhausted"} {
		key := "controlled-pilot:" + code
		if active[code] {
			if _, loaded := app.operationalAlertStates.LoadOrStore(key, struct{}{}); !loaded {
				log.Printf("event=opl_controlled_pilot_state code=%s state=active", code)
			}
			continue
		}
		if _, loaded := app.operationalAlertStates.LoadAndDelete(key); loaded {
			log.Printf("event=opl_controlled_pilot_state code=%s state=recovered", code)
		}
	}
}
