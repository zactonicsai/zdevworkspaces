{{- define "dev-pod.fullname" -}}
ws-{{ .Values.name }}
{{- end -}}

{{- define "dev-pod.labels" -}}
app.kubernetes.io/name: dev-pod
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/part-of: {{ .Values.partOf }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
helm.sh/chart: {{ .Chart.Name }}-{{ .Chart.Version }}
workspace/name: {{ .Values.name | quote }}
workspace/owner: {{ .Values.owner | quote }}
workspace/runtime: {{ .Values.runtime | quote }}
workspace/size: {{ .Values.size | quote }}
{{- end -}}

{{- define "dev-pod.selectorLabels" -}}
app.kubernetes.io/name: dev-pod
app.kubernetes.io/part-of: {{ .Values.partOf }}
workspace/name: {{ .Values.name | quote }}
{{- end -}}
