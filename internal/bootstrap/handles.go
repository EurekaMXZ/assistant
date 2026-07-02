package bootstrap

type resources struct {
	closeFns []func()
}

func (r *resources) addClose(fn func()) {
	if r == nil || fn == nil {
		return
	}
	r.closeFns = append(r.closeFns, fn)
}

func (r *resources) close() {
	if r == nil {
		return
	}

	for i := len(r.closeFns) - 1; i >= 0; i-- {
		r.closeFns[i]()
	}
}

type APIRuntime struct {
	resources  *resources
	server     serverRunner
	stopServer func()
	address    string
}

func (r *APIRuntime) Close() {
	if r == nil {
		return
	}
	if r.stopServer != nil {
		r.stopServer()
	}
	r.resources.close()
}

type WorkerRuntime struct {
	resources *resources
	worker    workerRunner
}

func (r *WorkerRuntime) Close() {
	if r == nil {
		return
	}
	r.resources.close()
}

type BackendRuntime struct {
	resources  *resources
	server     serverRunner
	worker     workerRunner
	stopServer func()
	address    string
}

func (r *BackendRuntime) Close() {
	if r == nil {
		return
	}
	if r.stopServer != nil {
		r.stopServer()
	}
	r.resources.close()
}
