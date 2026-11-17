{{/* Expand the name of the chart. */}}
{{- define "tinfoil-proxy.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/* Fullname of the release. */}}
{{- define "tinfoil-proxy.fullname" -}}
{{- if .Values.fullnameOverride }}
{{- .Values.fullnameOverride | trunc 63 | trimSuffix "-" }}
{{- else }}
{{- printf "%s-%s" .Release.Name (include "tinfoil-proxy.name" .) | trunc 63 | trimSuffix "-" }}
{{- end }}
{{- end }}

{{/* Common labels. */}}
{{- define "tinfoil-proxy.labels" -}}
helm.sh/chart: {{ printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{ include "tinfoil-proxy.selectorLabels" . }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end }}

{{/* Selector labels. */}}
{{- define "tinfoil-proxy.selectorLabels" -}}
app.kubernetes.io/name: {{ include "tinfoil-proxy.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}

{{/* Service account name. */}}
{{- define "tinfoil-proxy.serviceAccountName" -}}
{{- if .Values.serviceAccount.create }}
{{- default (include "tinfoil-proxy.fullname" .) .Values.serviceAccount.name }}
{{- else }}
{{- default "default" .Values.serviceAccount.name }}
{{- end }}
{{- end }}
