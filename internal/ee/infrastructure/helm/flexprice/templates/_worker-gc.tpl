{{/* Worker-only GC settings; fail rather than silently use a memory request. */}}
{{- define "flexprice.workerGCEnv" -}}
{{- $gc := .Values.worker.gc | default dict -}}
{{- if $gc.enabled -}}
{{- $ratio := float64 $gc.memoryLimitRatio -}}
{{- if or (le $ratio 0.0) (ge $ratio 1.0) -}}
{{- fail "worker.gc.memoryLimitRatio must be greater than 0 and less than 1" -}}
{{- end -}}
{{- $limit := dig "limits" "memory" "" .Values.worker.resources | toString -}}
{{- if not (regexMatch "^[0-9]+(\\.[0-9]+)?(Ki|Mi|Gi|Ti|Pi|Ei|k|M|G|T|P|E)?$" $limit) -}}
{{- fail "worker.gc requires worker.resources.limits.memory in bytes or Ki/Mi/Gi/Ti/Pi/Ei/k/M/G/T/P/E units" -}}
{{- end -}}
{{- $number := regexFind "^[0-9]+(\\.[0-9]+)?" $limit -}}
{{- $unit := trimPrefix $number $limit -}}
{{- $multipliers := dict "" 1 "Ki" 1024 "Mi" 1048576 "Gi" 1073741824 "Ti" 1099511627776 "Pi" 1125899906842624 "Ei" 1152921504606846976 "k" 1000 "M" 1000000 "G" 1000000000 "T" 1000000000000 "P" 1000000000000000 "E" 1000000000000000000 -}}
{{- $bytes := mulf (float64 $number) (float64 (index $multipliers $unit)) $ratio | floor -}}
{{- if or (lt $bytes 1.0) (ge $bytes 9223372036854775807.0) -}}
{{- fail "worker.gc calculated memory limit must fit in a positive int64" -}}
{{- end -}}
{{- $gogc := $gc.gogc | toString -}}
{{- if not (regexMatch "^[0-9]+$" $gogc) -}}
{{- fail "worker.gc.gogc must be a nonnegative integer" -}}
{{- end -}}
{{- range $name := list "GOMEMLIMIT" "GOGC" -}}
{{- if hasKey ($.Values.env | default dict) $name -}}
{{- fail (printf "worker.gc conflicts with global env.%s; use worker.gc settings instead" $name) -}}
{{- end -}}
{{- if hasKey ($.Values.secretEnv | default dict) $name -}}
{{- fail (printf "worker.gc conflicts with global secretEnv.%s" $name) -}}
{{- end -}}
{{- range $.Values.extraEnv -}}
{{- if eq .name $name -}}
{{- fail (printf "worker.gc conflicts with global extraEnv %s" $name) -}}
{{- end -}}
{{- end -}}
{{- end -}}
- name: GOMEMLIMIT
  value: {{ printf "%.0f" $bytes | quote }}
- name: GOGC
  value: {{ $gogc | quote }}
{{- end -}}
{{- end -}}
