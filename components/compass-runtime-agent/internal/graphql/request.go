package graphql

//import (
//	"strings"
//
//	"github.com/machinebox/graphql"
//)

//type Request struct {
//	query string
//	vars  map[string]interface{}
//	req   *graphql.Request
//}
//
//func NewRequest(q string) *graphql.Request {
//	query := strings.Replace(q, "\t", " ", -1)
//	return graphql.NewRequest(query)
//}

//func (r *Request) SetVar(key string, value interface{}) {
//	r.vars[key] = value
//	r.req.Var(key, value)
//}
//
//func (r *Request) AddHeader(key, value string) {
//	r.req.Header.Add(key, value)
//}
//
//func (r *Request) JSON() ([]byte, error) {
//	requestBodyObj := struct {
//		Query     string                 `json:"query"`
//		Variables map[string]interface{} `json:"variables"`
//	}{
//		Query:     r.query,
//		Variables: r.vars,
//	}
//
//	return json.Marshal(requestBodyObj)
//}
