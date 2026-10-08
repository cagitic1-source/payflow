package metrics

import "errors"

// ErrEmptyVersion - версия сборки не задана. Метка с пустым значением
// в Prometheus равна отсутствию метки, и payflow_build_info потеряла бы
// версию молча.
var ErrEmptyVersion = errors.New("build version is empty")
