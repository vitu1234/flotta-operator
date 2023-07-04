// Code generated by go-swagger; DO NOT EDIT.

package backend

// This file was generated by the swagger tool.
// Editing this file might prove futile when you re-run the swagger generate command

import (
	"fmt"
	"io"

	"github.com/go-openapi/runtime"
	"github.com/go-openapi/strfmt"

	"github.com/project-flotta/flotta-operator/backend/models"
)

// UpdateHeartBeatReader is a Reader for the UpdateHeartBeat structure.
type UpdateHeartBeatReader struct {
	formats strfmt.Registry
}

// ReadResponse reads a server response into the received o.
func (o *UpdateHeartBeatReader) ReadResponse(response runtime.ClientResponse, consumer runtime.Consumer) (interface{}, error) {
	switch response.Code() {
	case 200:
		result := NewUpdateHeartBeatOK()
		if err := result.readResponse(response, consumer, o.formats); err != nil {
			return nil, err
		}
		return result, nil
	case 401:
		result := NewUpdateHeartBeatUnauthorized()
		if err := result.readResponse(response, consumer, o.formats); err != nil {
			return nil, err
		}
		return nil, result
	case 403:
		result := NewUpdateHeartBeatForbidden()
		if err := result.readResponse(response, consumer, o.formats); err != nil {
			return nil, err
		}
		return nil, result
	default:
		result := NewUpdateHeartBeatDefault(response.Code())
		if err := result.readResponse(response, consumer, o.formats); err != nil {
			return nil, err
		}
		if response.Code()/100 == 2 {
			return result, nil
		}
		return nil, result
	}
}

// NewUpdateHeartBeatOK creates a UpdateHeartBeatOK with default headers values
func NewUpdateHeartBeatOK() *UpdateHeartBeatOK {
	return &UpdateHeartBeatOK{}
}

/*
	UpdateHeartBeatOK describes a response with status code 200, with default header values.

Success
*/
type UpdateHeartBeatOK struct {
}

func (o *UpdateHeartBeatOK) Error() string {
	return fmt.Sprintf("[PUT /namespaces/{namespace}/devices/{device-id}/heartbeat][%d] updateHeartBeatOK ", 200)
}

func (o *UpdateHeartBeatOK) readResponse(response runtime.ClientResponse, consumer runtime.Consumer, formats strfmt.Registry) error {

	return nil
}

// NewUpdateHeartBeatUnauthorized creates a UpdateHeartBeatUnauthorized with default headers values
func NewUpdateHeartBeatUnauthorized() *UpdateHeartBeatUnauthorized {
	return &UpdateHeartBeatUnauthorized{}
}

/*
	UpdateHeartBeatUnauthorized describes a response with status code 401, with default header values.

Unauthorized
*/
type UpdateHeartBeatUnauthorized struct {
}

func (o *UpdateHeartBeatUnauthorized) Error() string {
	return fmt.Sprintf("[PUT /namespaces/{namespace}/devices/{device-id}/heartbeat][%d] updateHeartBeatUnauthorized ", 401)
}

func (o *UpdateHeartBeatUnauthorized) readResponse(response runtime.ClientResponse, consumer runtime.Consumer, formats strfmt.Registry) error {

	return nil
}

// NewUpdateHeartBeatForbidden creates a UpdateHeartBeatForbidden with default headers values
func NewUpdateHeartBeatForbidden() *UpdateHeartBeatForbidden {
	return &UpdateHeartBeatForbidden{}
}

/*
	UpdateHeartBeatForbidden describes a response with status code 403, with default header values.

Forbidden
*/
type UpdateHeartBeatForbidden struct {
}

func (o *UpdateHeartBeatForbidden) Error() string {
	return fmt.Sprintf("[PUT /namespaces/{namespace}/devices/{device-id}/heartbeat][%d] updateHeartBeatForbidden ", 403)
}

func (o *UpdateHeartBeatForbidden) readResponse(response runtime.ClientResponse, consumer runtime.Consumer, formats strfmt.Registry) error {

	return nil
}

// NewUpdateHeartBeatDefault creates a UpdateHeartBeatDefault with default headers values
func NewUpdateHeartBeatDefault(code int) *UpdateHeartBeatDefault {
	return &UpdateHeartBeatDefault{
		_statusCode: code,
	}
}

/*
	UpdateHeartBeatDefault describes a response with status code -1, with default header values.

Error
*/
type UpdateHeartBeatDefault struct {
	_statusCode int

	Payload *models.Error
}

// Code gets the status code for the update heart beat default response
func (o *UpdateHeartBeatDefault) Code() int {
	return o._statusCode
}

func (o *UpdateHeartBeatDefault) Error() string {
	return fmt.Sprintf("[PUT /namespaces/{namespace}/devices/{device-id}/heartbeat][%d] UpdateHeartBeat default  %+v", o._statusCode, o.Payload)
}
func (o *UpdateHeartBeatDefault) GetPayload() *models.Error {
	return o.Payload
}

func (o *UpdateHeartBeatDefault) readResponse(response runtime.ClientResponse, consumer runtime.Consumer, formats strfmt.Registry) error {

	o.Payload = new(models.Error)

	// response payload
	if err := consumer.Consume(response.Body(), o.Payload); err != nil && err != io.EOF {
		return err
	}

	return nil
}
