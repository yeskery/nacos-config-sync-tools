package main

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"net/http"
	"reflect"
	"strings"
)

const rpcBridgeScript = `<script>
(function() {
  if (window.go && window.go.main && window.go.main.App) return;
  const appProxy = new Proxy({}, {
    get(_, method) {
      if (method === "then" || typeof method === "symbol") return undefined;
      return async (...args) => {
        const resp = await fetch("/api/rpc", {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify({ method, args })
        });
        const data = await resp.json();
        if (!resp.ok || data.error) {
          throw new Error(data.error || resp.statusText);
        }
        return data.result;
      };
    }
  });
  window.go = { main: { App: appProxy } };
})();
</script>`

type rpcRequest struct {
	Method string            `json:"method"`
	Args   []json.RawMessage `json:"args"`
}

type rpcResponse struct {
	Result any    `json:"result,omitempty"`
	Error  string `json:"error,omitempty"`
}

func newWebHandler(app *App, distFS fs.FS) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/rpc", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			_ = json.NewEncoder(w).Encode(rpcResponse{Error: "method not allowed"})
			return
		}
		var req rpcRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(rpcResponse{Error: err.Error()})
			return
		}
		res, err := invokeAppMethod(app, req.Method, req.Args)
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(rpcResponse{Error: err.Error()})
			return
		}
		_ = json.NewEncoder(w).Encode(rpcResponse{Result: res})
	})

	fileServer := http.FileServer(http.FS(distFS))
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" || r.URL.Path == "/index.html" {
			data, err := fs.ReadFile(distFS, "index.html")
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			html := strings.Replace(string(data), "</head>", rpcBridgeScript+"\n  </head>", 1)
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = w.Write([]byte(html))
			return
		}
		fileServer.ServeHTTP(w, r)
	})
	return mux
}

func invokeAppMethod(app *App, methodName string, rawArgs []json.RawMessage) (any, error) {
	if methodName == "" {
		return nil, fmt.Errorf("method name is required")
	}
	method := reflect.ValueOf(app).MethodByName(methodName)
	if !method.IsValid() {
		return nil, fmt.Errorf("unknown method %q", methodName)
	}
	methodType := method.Type()
	if len(rawArgs) != methodType.NumIn() {
		return nil, fmt.Errorf("method %s expects %d arguments, got %d", methodName, methodType.NumIn(), len(rawArgs))
	}
	in := make([]reflect.Value, methodType.NumIn())
	for i := 0; i < methodType.NumIn(); i++ {
		argPtr := reflect.New(methodType.In(i))
		if len(rawArgs[i]) > 0 {
			if err := json.Unmarshal(rawArgs[i], argPtr.Interface()); err != nil {
				return nil, fmt.Errorf("invalid argument %d for %s: %w", i, methodName, err)
			}
		}
		in[i] = argPtr.Elem()
	}
	out := method.Call(in)
	var result any
	for _, val := range out {
		if err, ok := val.Interface().(error); ok && err != nil {
			return nil, err
		}
		if val.Type().Implements(reflect.TypeOf((*error)(nil)).Elem()) {
			continue
		}
		result = val.Interface()
	}
	return result, nil
}
