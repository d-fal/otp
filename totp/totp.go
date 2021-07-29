/**
 *  Copyright 2014 Paul Querna
 *
 *  Licensed under the Apache License, Version 2.0 (the "License");
 *  you may not use this file except in compliance with the License.
 *  You may obtain a copy of the License at
 *
 *      http://www.apache.org/licenses/LICENSE-2.0
 *
 *  Unless required by applicable law or agreed to in writing, software
 *  distributed under the License is distributed on an "AS IS" BASIS,
 *  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 *  See the License for the specific language governing permissions and
 *  limitations under the License.
 *
 */

package totp

import (
	"io"

	"github.com/pquerna/otp"
	"github.com/pquerna/otp/hotp"

	"crypto/rand"
	"encoding/base32"
	"math"
	"net/url"
	"strconv"
	"time"
)

// Validate a TOTP using the current time.
// A shortcut for ValidateCustom, Validate uses a configuration
// that is compatible with Google-Authenticator and most clients.
func Validate(passcode string, secret string, validateOpts ...ValidateOpt) bool {
	rv, _ := ValidateCustom(
		passcode,
		secret,
		time.Now().UTC(),
		validateOpts...,
	)
	return rv
}

// GenerateCode creates a TOTP token using the current time.
// A shortcut for GenerateCodeCustom, GenerateCode uses a configuration
// that is compatible with Google-Authenticator and most clients.
func GenerateCode(secret string, t time.Time) (string, error) {
	return GenerateCodeCustom(secret,
		t,
		WithDigits(otp.DigitsSix),
		WithSkew(1),
		WithAlgorithm(otp.AlgorithmSHA1),
		WithPeriod(30),
	)
}

// ValidateOpts provides options for ValidateCustom().
type ValidateOpts struct {
	// Number of seconds a TOTP hash is valid for. Defaults to 30 seconds.
	Period uint
	// Periods before or after the current time to allow.  Value of 1 allows up to Period
	// of either side of the specified time.  Defaults to 0 allowed skews.  Values greater
	// than 1 are likely sketchy.
	Skew uint
	// Digits as part of the input. Defaults to 6.
	Digits otp.Digits
	// Algorithm to use for HMAC. Defaults to SHA1.
	Algorithm otp.Algorithm
}

func (opts *ValidateOpts) defaultOpts() {
	if opts.Skew == 0 {
		opts.Skew = 30
	}
	if opts.Digits == 0 {
		opts.Digits = otp.DigitsSix
	}
	if opts.Period == 0 {
		opts.Period = 30
	}
}

type ValidateOpt func(opt *ValidateOpts)

func WithPeriod(period uint) ValidateOpt {
	return func(opt *ValidateOpts) {
		opt.Period = period
	}
}
func WithSkew(skew uint) ValidateOpt {
	return func(opt *ValidateOpts) {
		opt.Skew = skew
	}
}

func WithDigits(digits otp.Digits) ValidateOpt {
	return func(opt *ValidateOpts) {
		opt.Digits = digits
	}
}

func WithAlgorithm(algo otp.Algorithm) ValidateOpt {
	return func(opt *ValidateOpts) {
		opt.Algorithm = algo
	}
}

// GenerateCodeCustom takes a timepoint and produces a passcode using a
// secret and the provided opts. (Under the hood, this is making an adapted
// call to hotp.GenerateCodeCustom)
func GenerateCodeCustom(secret string, t time.Time, validateOpts ...ValidateOpt) (passcode string, err error) {

	opts := new(ValidateOpts)

	for _, opt := range validateOpts {
		opt(opts)
	}
	opts.defaultOpts()

	counter := uint64(math.Floor(float64(t.Unix()) / float64(opts.Period)))
	passcode, err = hotp.GenerateCodeCustom(secret, counter, hotp.ValidateOpts{
		Digits:    opts.Digits,
		Algorithm: opts.Algorithm,
	})
	if err != nil {
		return "", err
	}
	return passcode, nil
}

// ValidateCustom validates a TOTP given a user specified time and custom options.
// Most users should use Validate() to provide an interpolatable TOTP experience.
func ValidateCustom(passcode string, secret string, t time.Time, validateOpts ...ValidateOpt) (bool, error) {

	opts := new(ValidateOpts)

	for _, opt := range validateOpts {
		opt(opts)
	}
	opts.defaultOpts()

	counters := []uint64{}
	counter := int64(math.Floor(float64(t.Unix()) / float64(opts.Period)))

	counters = append(counters, uint64(counter))
	for i := 1; i <= int(opts.Skew); i++ {
		counters = append(counters, uint64(counter+int64(i)))
		counters = append(counters, uint64(counter-int64(i)))
	}

	for _, counter := range counters {
		rv, err := hotp.ValidateCustom(passcode, counter, secret, hotp.ValidateOpts{
			Digits:    opts.Digits,
			Algorithm: opts.Algorithm,
		})

		if err != nil {
			return false, err
		}

		if rv == true {
			return true, nil
		}
	}

	return false, nil
}

// GenerateOpts provides options for Generate().  The default values
// are compatible with Google-Authenticator.
type GenerateOpts struct {
	// Name of the issuing Organization/Company.
	Issuer string
	// Name of the User's Account (eg, email address)
	AccountName string
	// Number of seconds a TOTP hash is valid for. Defaults to 30 seconds.
	Period uint
	// Size in size of the generated Secret. Defaults to 20 bytes.
	SecretSize uint
	// Secret to store. Defaults to a randomly generated secret of SecretSize.  You should generally leave this empty.
	Secret []byte
	// Digits to request. Defaults to 6.
	Digits otp.Digits
	// Algorithm to use for HMAC. Defaults to SHA1.
	Algorithm otp.Algorithm
	// Reader to use for generating TOTP Key.
	Rand io.Reader
}

var b32NoPadding = base32.StdEncoding.WithPadding(base32.NoPadding)

// Generate a new TOTP Key.
func Generate(opts GenerateOpts) (*otp.Key, error) {
	// url encode the Issuer/AccountName
	if opts.Issuer == "" {
		return nil, otp.ErrGenerateMissingIssuer
	}

	if opts.AccountName == "" {
		return nil, otp.ErrGenerateMissingAccountName
	}

	if opts.Period == 0 {
		opts.Period = 30
	}

	if opts.SecretSize == 0 {
		opts.SecretSize = 20
	}

	if opts.Digits == 0 {
		opts.Digits = otp.DigitsSix
	}

	if opts.Rand == nil {
		opts.Rand = rand.Reader
	}

	// otpauth://totp/Example:alice@google.com?secret=JBSWY3DPEHPK3PXP&issuer=Example

	v := url.Values{}
	if len(opts.Secret) != 0 {
		v.Set("secret", b32NoPadding.EncodeToString(opts.Secret))
	} else {
		secret := make([]byte, opts.SecretSize)
		_, err := opts.Rand.Read(secret)
		if err != nil {
			return nil, err
		}
		v.Set("secret", b32NoPadding.EncodeToString(secret))
	}

	v.Set("issuer", opts.Issuer)
	v.Set("period", strconv.FormatUint(uint64(opts.Period), 10))
	v.Set("algorithm", opts.Algorithm.String())
	v.Set("digits", opts.Digits.String())

	u := url.URL{
		Scheme:   "otpauth",
		Host:     "totp",
		Path:     "/" + opts.Issuer + ":" + opts.AccountName,
		RawQuery: v.Encode(),
	}

	return otp.NewKeyFromURL(u.String())
}
