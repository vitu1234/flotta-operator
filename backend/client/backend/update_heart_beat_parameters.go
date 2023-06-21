// Code generated by go-swagger; DO NOT EDIT.

package backend

// This file was generated by the swagger tool.
// Editing this file might prove futile when you re-run the swagger generate command

import (
	"context"
	"net/http"
	"time"

	"github.com/go-openapi/errors"
	"github.com/go-openapi/runtime"
	cr "github.com/go-openapi/runtime/client"
	"github.com/go-openapi/strfmt"

	commonmodel "github.com/project-flotta/flotta-operator/models"
)

// NewUpdateHeartBeatParams creates a new UpdateHeartBeatParams object,
// with the default timeout for this client.
//
// Default values are not hydrated, since defaults are normally applied by the API server side.
//
// To enforce default values in parameter, use SetDefaults or WithDefaults.
func NewUpdateHeartBeatParams() *UpdateHeartBeatParams {
	return &UpdateHeartBeatParams{
		timeout: cr.DefaultTimeout,
	}
}

// NewUpdateHeartBeatParamsWithTimeout creates a new UpdateHeartBeatParams object
// with the ability to set a timeout on a request.
func NewUpdateHeartBeatParamsWithTimeout(timeout time.Duration) *UpdateHeartBeatParams {
	return &UpdateHeartBeatParams{
		timeout: timeout,
	}
}

// NewUpdateHeartBeatParamsWithContext creates a new UpdateHeartBeatParams object
// with the ability to set a context for a request.
func NewUpdateHeartBeatParamsWithContext(ctx context.Context) *UpdateHeartBeatParams {
	return &UpdateHeartBeatParams{
		Context: ctx,
	}
}

// NewUpdateHeartBeatParamsWithHTTPClient creates a new UpdateHeartBeatParams object
// with the ability to set a custom HTTPClient for a request.
func NewUpdateHeartBeatParamsWithHTTPClient(client *http.Client) *UpdateHeartBeatParams {
	return &UpdateHeartBeatParams{
		HTTPClient: client,
	}
}

/* UpdateHeartBeatParams contains all the parameters to send to the API endpoint
   for the update heart beat operation.

   Typically these are written to a http.Request.
*/
type UpdateHeartBeatParams struct {

	/* DeviceID.

	   Device ID
	*/
	DeviceID string

	// Heartbeat.
	Heartbeat commonmodel.Heartbeat

	/* Namespace.

	   Namespace where the device resides
	*/
	Namespace string

	timeout    time.Duration
	Context    context.Context
	HTTPClient *http.Client
}

// WithDefaults hydrates default values in the update heart beat params (not the query body).
//
// All values with no default are reset to their zero value.
func (o *UpdateHeartBeatParams) WithDefaults() *UpdateHeartBeatParams {
	o.SetDefaults()
	return o
}

// SetDefaults hydrates default values in the update heart beat params (not the query body).
//
// All values with no default are reset to their zero value.
func (o *UpdateHeartBeatParams) SetDefaults() {
	// no default values defined for this parameter
}

// WithTimeout adds the timeout to the update heart beat params
func (o *UpdateHeartBeatParams) WithTimeout(timeout time.Duration) *UpdateHeartBeatParams {
	o.SetTimeout(timeout)
	return o
}

// SetTimeout adds the timeout to the update heart beat params
func (o *UpdateHeartBeatParams) SetTimeout(timeout time.Duration) {
	o.timeout = timeout
}

// WithContext adds the context to the update heart beat params
func (o *UpdateHeartBeatParams) WithContext(ctx context.Context) *UpdateHeartBeatParams {
	o.SetContext(ctx)
	return o
}

// SetContext adds the context to the update heart beat params
func (o *UpdateHeartBeatParams) SetContext(ctx context.Context) {
	o.Context = ctx
}

// WithHTTPClient adds the HTTPClient to the update heart beat params
func (o *UpdateHeartBeatParams) WithHTTPClient(client *http.Client) *UpdateHeartBeatParams {
	o.SetHTTPClient(client)
	return o
}

// SetHTTPClient adds the HTTPClient to the update heart beat params
func (o *UpdateHeartBeatParams) SetHTTPClient(client *http.Client) {
	o.HTTPClient = client
}

// WithDeviceID adds the deviceID to the update heart beat params
func (o *UpdateHeartBeatParams) WithDeviceID(deviceID string) *UpdateHeartBeatParams {
	o.SetDeviceID(deviceID)
	return o
}

// SetDeviceID adds the deviceId to the update heart beat params
func (o *UpdateHeartBeatParams) SetDeviceID(deviceID string) {
	o.DeviceID = deviceID
}

// WithHeartbeat adds the heartbeat to the update heart beat params
func (o *UpdateHeartBeatParams) WithHeartbeat(heartbeat commonmodel.Heartbeat) *UpdateHeartBeatParams {
	o.SetHeartbeat(heartbeat)
	return o
}

// SetHeartbeat adds the heartbeat to the update heart beat params
func (o *UpdateHeartBeatParams) SetHeartbeat(heartbeat commonmodel.Heartbeat) {
	o.Heartbeat = heartbeat
}

// WithNamespace adds the namespace to the update heart beat params
func (o *UpdateHeartBeatParams) WithNamespace(namespace string) *UpdateHeartBeatParams {
	o.SetNamespace(namespace)
	return o
}

// SetNamespace adds the namespace to the update heart beat params
func (o *UpdateHeartBeatParams) SetNamespace(namespace string) {
	o.Namespace = namespace
}

// WriteToRequest writes these params to a swagger request
func (o *UpdateHeartBeatParams) WriteToRequest(r runtime.ClientRequest, reg strfmt.Registry) error {

	if err := r.SetTimeout(o.timeout); err != nil {
		return err
	}
	var res []error

	// path param device-id
	if err := r.SetPathParam("device-id", o.DeviceID); err != nil {
		return err
	}
	if err := r.SetBodyParam(o.Heartbeat); err != nil {
		return err
	}

	// path param namespace
	if err := r.SetPathParam("namespace", o.Namespace); err != nil {
		return err
	}

	if len(res) > 0 {
		return errors.CompositeValidationError(res...)
	}
	return nil
}