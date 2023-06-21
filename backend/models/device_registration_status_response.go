// Code generated by go-swagger; DO NOT EDIT.

package models

// This file was generated by the swagger tool.
// Editing this file might prove futile when you re-run the swagger generate command

import (
	"context"

	"github.com/go-openapi/strfmt"
	"github.com/go-openapi/swag"
)

// DeviceRegistrationStatusResponse device registration status response
//
// swagger:model device-registration-status-response
type DeviceRegistrationStatusResponse struct {

	// Exposes the error message generated at the backend when there is an error (example HTTP code 500).
	Message string `json:"message,omitempty"`

	// Namespace the device should be or was finally placed during registration.
	Namespace string `json:"namespace,omitempty"`

	// Returns the device registration status, which can be one of the following {registered, unregistered, unknown}.
	Status string `json:"status,omitempty"`
}

// Validate validates this device registration status response
func (m *DeviceRegistrationStatusResponse) Validate(formats strfmt.Registry) error {
	return nil
}

// ContextValidate validates this device registration status response based on context it is used
func (m *DeviceRegistrationStatusResponse) ContextValidate(ctx context.Context, formats strfmt.Registry) error {
	return nil
}

// MarshalBinary interface implementation
func (m *DeviceRegistrationStatusResponse) MarshalBinary() ([]byte, error) {
	if m == nil {
		return nil, nil
	}
	return swag.WriteJSON(m)
}

// UnmarshalBinary interface implementation
func (m *DeviceRegistrationStatusResponse) UnmarshalBinary(b []byte) error {
	var res DeviceRegistrationStatusResponse
	if err := swag.ReadJSON(b, &res); err != nil {
		return err
	}
	*m = res
	return nil
}