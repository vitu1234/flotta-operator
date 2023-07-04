// Code generated by go-swagger; DO NOT EDIT.

package backend

// This file was generated by the swagger tool.
// Editing this file might prove futile when you re-run the generate command

import (
	"net/http"

	"github.com/go-openapi/runtime/middleware"
)

// RegisterDeviceHandlerFunc turns a function with the right signature into a register device handler
type RegisterDeviceHandlerFunc func(RegisterDeviceParams) middleware.Responder

// Handle executing the request and returning a response
func (fn RegisterDeviceHandlerFunc) Handle(params RegisterDeviceParams) middleware.Responder {
	return fn(params)
}

// RegisterDeviceHandler interface for that can handle valid register device params
type RegisterDeviceHandler interface {
	Handle(RegisterDeviceParams) middleware.Responder
}

// NewRegisterDevice creates a new http.Handler for the register device operation
func NewRegisterDevice(ctx *middleware.Context, handler RegisterDeviceHandler) *RegisterDevice {
	return &RegisterDevice{Context: ctx, Handler: handler}
}

/*
	RegisterDevice swagger:route PUT /namespaces/{namespace}/devices/{device-id}/registration backend registerDevice

Registers the device by providing its hardware configuration
*/
type RegisterDevice struct {
	Context *middleware.Context
	Handler RegisterDeviceHandler
}

func (o *RegisterDevice) ServeHTTP(rw http.ResponseWriter, r *http.Request) {
	route, rCtx, _ := o.Context.RouteInfo(r)
	if rCtx != nil {
		*r = *rCtx
	}
	var Params = NewRegisterDeviceParams()
	if err := o.Context.BindValidRequest(r, route, &Params); err != nil { // bind params
		o.Context.Respond(rw, r, route.Produces, route, err)
		return
	}

	res := o.Handler.Handle(Params) // actually handle the request
	o.Context.Respond(rw, r, route.Produces, route, res)

}
