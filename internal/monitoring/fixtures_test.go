package monitoring

import "time"

// Deterministic alert fixtures. Each is built by a function so a test that
// mutates what it is handed cannot leak into the next one.

func fxStart() time.Time { return time.Date(2026, 9, 23, 6, 41, 0, 0, time.UTC) }

// fxCrashLoop is the everyday case: a pod alert whose labels resolve cleanly
// to a kind, a name, a namespace and a reason.
func fxCrashLoop() Alert {
	return Alert{
		Name:        "KubePodCrashLooping-p0",
		State:       "firing",
		Severity:    "critical",
		Summary:     "Pod pgvector-6cccdcb6f6-cgdhj in namespace devtroncd is CrashLoopBackOff",
		Description: "Pod has restarted 540 times in the last 45h",
		Labels: map[string]string{
			"alertname": "KubePodCrashLooping-p0",
			"container": "pgvector",
			"job":       "kube-state-metrics",
			"namespace": "devtroncd",
			"pod":       "pgvector-6cccdcb6f6-cgdhj",
			"reason":    "CrashLoopBackOff",
			"severity":  "critical",
		},
		StartsAt:   fxStart(),
		Source:     "alertmanager",
		Expression: `kube_pod_container_status_waiting_reason{reason="CrashLoopBackOff"} > 0`,
		Namespace:  "devtroncd",
		Kind:       "Pod",
		Resource:   "pgvector-6cccdcb6f6-cgdhj",
	}
}

// fxNoResource is a cluster-wide alert: nothing in the labels names an object,
// so the alert name is the only handle there is.
func fxNoResource() Alert {
	return Alert{
		Name:     "KubeSchedulerDown",
		State:    "firing",
		Severity: "critical",
		Summary:  "Target disappeared from Prometheus target discovery",
		Labels:   map[string]string{"alertname": "KubeSchedulerDown", "severity": "critical"},
		StartsAt: fxStart(),
		Source:   "alertmanager",
	}
}

// fxPhaseOnly has no reason label; the condition has to come from phase.
func fxPhaseOnly() Alert {
	return Alert{
		Name:      "KubePodNotReady",
		State:     "firing",
		Labels:    map[string]string{"phase": "Pending", "namespace": "prod"},
		Namespace: "prod",
		Kind:      "Pod",
		Resource:  "api-7d9f",
	}
}

// fxBlankCondition has condition labels present but empty, which is the shape
// that turns a question into "… is in ?" if the emptiness is not checked.
func fxBlankCondition() Alert {
	return Alert{
		Name:      "TargetDown",
		State:     "firing",
		Labels:    map[string]string{"reason": "   ", "phase": ""},
		Namespace: "monitoring",
		Kind:      "Service",
		Resource:  "node-exporter",
	}
}

// fxUnicode exercises non-ASCII names, which reach the prompt verbatim.
func fxUnicode() Alert {
	return Alert{
		Name:      "アラート発生",
		State:     "firing",
		Labels:    map[string]string{"reason": "メモリ不足"},
		Namespace: "本番",
		Kind:      "Pod",
		Resource:  "サービス-01",
	}
}

// fxEmpty is the degenerate alert: nothing resolved at all.
func fxEmpty() Alert { return Alert{} }
