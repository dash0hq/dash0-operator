# SPDX-FileCopyrightText: Copyright 2025 Dash0 Inc.
# SPDX-License-Identifier: Apache-2.0

{{- define "jvm-test-app.chartName" -}}
{{- .Chart.Name | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Create a Service resource
*/}}
{{- define "jvm-test-app.service" -}}
---
apiVersion: v1
kind: Service
metadata:
  name: dash0-operator-jvm-spring-boot-test-{{ .workloadType }}-service
  {{- if .annotations }}
  annotations:
    {{- toYaml .annotations | nindent 4 }}
  {{- end }}
spec:
  selector:
    app: {{ .selector }}
  ports:
    - port: {{ .port }}
      targetPort: {{ .targetPort }}
  type: LoadBalancer
{{- end }}

{{/*
Create an Ingress resource
*/}}
{{- define "jvm-test-app.ingress" -}}
---
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: dash0-operator-jvm-spring-boot-test-{{ .workloadType }}-ingress
  annotations:
    nginx.ingress.kubernetes.io/use-regex: "true"
    nginx.ingress.kubernetes.io/rewrite-target: /$2
spec:
  rules:
  - http:
      paths:
      - path: {{ .path }}
        pathType: ImplementationSpecific
        backend:
          service:
            name: dash0-operator-jvm-spring-boot-test-{{ .workloadType }}-service
            port:
              number: {{ .port }}
{{- end }}
