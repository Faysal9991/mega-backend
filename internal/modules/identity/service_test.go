package identity

import (
	"context"
	"github.com/Faysal9991/mega-backend/internal/platform/apperror"
	"testing"
	"time"
)

type negativeRepository struct {
	Repository
	id, hash     string
	lookupErr    error
	proof        Proof
	transactions int
}

func (r *negativeRepository) FindIdentifier(context.Context, string, string) (string, error) {
	return r.id, r.lookupErr
}
func (r *negativeRepository) Password(context.Context, string) (string, error) { return r.hash, nil }
func (r *negativeRepository) Within(context.Context, func(Store) error) error {
	r.transactions++
	panic("unauthenticated flow entered transaction")
}
func (r *negativeRepository) Proof(context.Context, string) (Proof, error) { return r.proof, nil }
func (r *negativeRepository) Challenge(context.Context, string) (Challenge, error) {
	return Challenge{Hash: Digest("fixture"), Installation: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", Purpose: "login", Created: time.Now()}, nil
}

type rejectPasswords struct{ verifications int }

func (h *rejectPasswords) Hash(context.Context, string) (string, error) {
	panic("unauthenticated password setup hashed input")
}
func (h *rejectPasswords) Verify(context.Context, string, string) (bool, error) {
	h.verifications++
	return false, nil
}
func TestGenericPasswordFailureAndNoAccountCreation(t *testing.T) {
	for _, tc := range []struct {
		name, id, hash string
		err            error
	}{{"unknown", "", "", ErrMissing}, {"without_password", "account", "", nil}, {"incorrect", "account", "existing-hash", nil}} {
		t.Run(tc.name, func(t *testing.T) {
			repo := &negativeRepository{id: tc.id, hash: tc.hash, lookupErr: tc.err}
			hashes := &rejectPasswords{}
			s, err := New(repo, NewGoogle(""), hashes)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := s.PasswordLogin(context.Background(), "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", "synthetic", "incorrect password"); apperror.Public(err).Code() != apperror.Unauthenticated || err.Error() != "UNAUTHENTICATED" {
				t.Fatal("non-generic password failure", err)
			}
			if hashes.verifications != 1 || repo.transactions != 0 {
				t.Fatal("password login skipped verification or created an account")
			}
		})
	}
}
func TestPasswordProofBindingAndProviderFailure(t *testing.T) {
	repo := &negativeRepository{proof: Proof{Account: "account", Installation: "cccccccccccccccccccccccccccccccc", Purpose: "password"}}
	s, err := New(repo, NewGoogle(""), &rejectPasswords{})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetPassword(context.Background(), PasswordChange{Installation: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", Password: "synthetic new password", ProofToken: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}); apperror.Public(err).Code() != apperror.Unauthenticated {
		t.Fatal("proof crossed installation")
	}
	if _, err := s.Google(context.Background(), "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "fixture"); apperror.Public(err).Code() != apperror.ServiceUnavailable {
		t.Fatal("missing Google verifier created account")
	}
	if repo.transactions != 0 {
		t.Fatal("unauthorized flow mutated state")
	}
}
