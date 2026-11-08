{{- define "api.name" -}}dev-pods{{- end -}}
{{- define "api.fullname" -}}{{ .Release.Name }}{{- end -}}
{{- define "api.sa" -}}{{ default (include "api.fullname" .) .Values.serviceAccount.name }}{{- end -}}

{{- define "api.labels" -}}
app.kubernetes.io/name: {{ include "api.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
helm.sh/chart: {{ .Chart.Name }}-{{ .Chart.Version }}
{{- end -}}

{{- define "api.selectorLabels" -}}
app.kubernetes.io/name: {{ include "api.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end -}}
