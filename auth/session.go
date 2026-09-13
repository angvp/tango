package auth

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"reflect"
	"time"

	"github.com/angvp/tango/db"
	"github.com/angvp/tango/model"
)

var ErrInvalidSessionModel = errors.New("tango auth: invalid session model")

type sessionFields struct {
	primaryKey model.FieldMeta
	token      model.FieldMeta
	userID     model.FieldMeta
	expiresAt  model.FieldMeta
}

// ValidateSessionModel validates the fixed field-name convention auth's
// generic session helpers expect: a primary key, Token string, UserID as a
// foreign key, and ExpiresAt time.Time.
func ValidateSessionModel(sessionMeta model.ModelMeta) error {
	_, err := inspectSessionModel(sessionMeta)
	return err
}

// CreateSession creates a session row for userID with a crypto-random,
// cookie-safe token and the caller-supplied duration.
func CreateSession(ctx context.Context, store *db.Store, sessionMeta model.ModelMeta, userID any, duration time.Duration) (string, time.Time, error) {
	fields, err := inspectSessionModel(sessionMeta)
	if err != nil {
		return "", time.Time{}, err
	}
	token, err := randomToken()
	if err != nil {
		return "", time.Time{}, err
	}
	expiresAt := time.Now().UTC().Add(duration)

	instance := reflect.New(sessionMeta.Type)
	elem := instance.Elem()
	elem.FieldByName(fields.token.Name).SetString(token)
	if err := setReflectValue(elem.FieldByName(fields.userID.Name), userID); err != nil {
		return "", time.Time{}, fmt.Errorf("%w: UserID cannot accept %T", ErrInvalidSessionModel, userID)
	}
	elem.FieldByName(fields.expiresAt.Name).Set(reflect.ValueOf(expiresAt))

	if err := store.Create(ctx, sessionMeta, instance.Interface()); err != nil {
		return "", time.Time{}, err
	}
	return token, expiresAt, nil
}

// SessionUser returns the stored UserID for a valid, unexpired session.
// ok is false for empty, missing, or expired tokens.
func SessionUser(ctx context.Context, store *db.Store, sessionMeta model.ModelMeta, token string) (any, bool, error) {
	fields, err := inspectSessionModel(sessionMeta)
	if err != nil {
		return nil, false, err
	}
	if token == "" {
		return nil, false, nil
	}

	dest := reflect.New(reflect.SliceOf(sessionMeta.Type))
	sqlQuery := fmt.Sprintf(
		"SELECT %s AS %s, %s AS %s FROM %s WHERE %s = ?",
		db.ColumnName(fields.userID.Name), fields.userID.Name,
		db.ColumnName(fields.expiresAt.Name), fields.expiresAt.Name,
		db.ColumnName(sessionMeta.Name), db.ColumnName(fields.token.Name),
	)
	if err := store.Query(ctx, dest.Interface(), sqlQuery, token); err != nil {
		return nil, false, err
	}
	rows := dest.Elem()
	if rows.Len() == 0 {
		return nil, false, nil
	}
	session := rows.Index(0)
	expiresAt := session.FieldByName(fields.expiresAt.Name).Interface().(time.Time)
	if !expiresAt.After(time.Now().UTC()) {
		return nil, false, nil
	}
	return session.FieldByName(fields.userID.Name).Interface(), true, nil
}

// DeleteSession deletes every session row matching token. Missing tokens
// are a no-op.
func DeleteSession(ctx context.Context, store *db.Store, sessionMeta model.ModelMeta, token string) error {
	fields, err := inspectSessionModel(sessionMeta)
	if err != nil {
		return err
	}
	if token == "" {
		return nil
	}

	dest := reflect.New(reflect.SliceOf(sessionMeta.Type))
	sqlQuery := fmt.Sprintf(
		"SELECT %s AS %s FROM %s WHERE %s = ?",
		db.ColumnName(fields.primaryKey.Name), fields.primaryKey.Name,
		db.ColumnName(sessionMeta.Name), db.ColumnName(fields.token.Name),
	)
	if err := store.Query(ctx, dest.Interface(), sqlQuery, token); err != nil {
		return err
	}
	rows := dest.Elem()
	for i := 0; i < rows.Len(); i++ {
		pk := rows.Index(i).FieldByName(fields.primaryKey.Name).Interface()
		if err := store.Delete(ctx, sessionMeta, pk); err != nil {
			return err
		}
	}
	return nil
}

func inspectSessionModel(sessionMeta model.ModelMeta) (sessionFields, error) {
	var fields sessionFields
	var hasPK, hasToken, hasUserID, hasExpiresAt bool
	timeType := reflect.TypeOf(time.Time{})

	for _, field := range sessionMeta.Fields {
		if field.PrimaryKey {
			fields.primaryKey = field
			hasPK = true
		}
		switch field.Name {
		case "Token":
			fields.token = field
			hasToken = true
		case "UserID":
			fields.userID = field
			hasUserID = true
		case "ExpiresAt":
			fields.expiresAt = field
			hasExpiresAt = true
		}
	}

	switch {
	case !hasPK:
		return fields, fmt.Errorf("%w: %s missing primary key", ErrInvalidSessionModel, sessionMeta.Name)
	case !hasToken:
		return fields, fmt.Errorf("%w: %s missing Token string field", ErrInvalidSessionModel, sessionMeta.Name)
	case fields.token.Type.Kind() != reflect.String:
		return fields, fmt.Errorf("%w: %s.Token must be string", ErrInvalidSessionModel, sessionMeta.Name)
	case !hasUserID:
		return fields, fmt.Errorf("%w: %s missing UserID foreign key field", ErrInvalidSessionModel, sessionMeta.Name)
	case fields.userID.ForeignKey == "":
		return fields, fmt.Errorf("%w: %s.UserID must be tagged tango:\"fk=<UserModel>\"", ErrInvalidSessionModel, sessionMeta.Name)
	case !hasExpiresAt:
		return fields, fmt.Errorf("%w: %s missing ExpiresAt time.Time field", ErrInvalidSessionModel, sessionMeta.Name)
	case fields.expiresAt.Type != timeType:
		return fields, fmt.Errorf("%w: %s.ExpiresAt must be time.Time", ErrInvalidSessionModel, sessionMeta.Name)
	}
	return fields, nil
}

func setReflectValue(dest reflect.Value, value any) error {
	if value == nil {
		return fmt.Errorf("nil value")
	}
	source := reflect.ValueOf(value)
	if source.Type().AssignableTo(dest.Type()) {
		dest.Set(source)
		return nil
	}
	if source.Type().ConvertibleTo(dest.Type()) {
		dest.Set(source.Convert(dest.Type()))
		return nil
	}
	return fmt.Errorf("cannot set %s from %s", dest.Type(), source.Type())
}

func randomToken() (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}
