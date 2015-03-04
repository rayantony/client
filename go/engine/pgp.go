package engine

//
// engine.PGPEngine is a class for optionally generating PGP keys,
// and pushing them into the keybase sigchain via the Delegator.
//

import (
	stderrors "errors"
	"os/exec"
	"strings"

	"github.com/keybase/client/go/libkb"
	triplesec "github.com/keybase/go-triplesec"
)

type PGPEngine struct {
	me     *libkb.User
	bundle *libkb.PgpKeyBundle
	arg    PGPEngineArg
	epk    string
	del    *libkb.Delegator
}

type PGPEngineArg struct {
	Gen        *libkb.PGPGenArg
	Pregen     *libkb.PgpKeyBundle
	SigningKey libkb.GenericKey
	Me         *libkb.User
	NoSave     bool
	PushSecret bool
	AllowMulti bool
	DoExport   bool
}

func (s *PGPEngine) loadMe() (err error) {
	if s.me = s.arg.Me; s.me != nil {
		return
	}
	s.me, err = libkb.LoadMe(libkb.LoadUserArg{PublicKeyOptional: true})
	return err
}

func (s *PGPEngine) generateKey(ctx *Context) (err error) {
	gen := s.arg.Gen
	if err = gen.CreatePgpIDs(); err != nil {
		return
	}
	s.bundle, err = libkb.NewPgpKeyBundle(*gen, ctx.LogUI)
	return
}

func (s *PGPEngine) saveLKS(ctx *Context) (err error) {
	var lks *libkb.LKSec
	if lks, err = libkb.NewLKSForEncrypt(ctx.SecretUI); err != nil {
		return
	}
	_, err = libkb.WriteLksSKBToKeyring(s.me.GetName(), s.bundle, lks, ctx.LogUI)
	return
}

var ErrKeyGenArgNoDefNoCustom = stderrors.New("invalid args:  NoDefPGPUid set, but no custom PGPUids.")

func NewPGPEngine(arg PGPEngineArg) *PGPEngine {
	return &PGPEngine{arg: arg}
}

func (s *PGPEngine) Name() string {
	return "PGP"
}

func (e *PGPEngine) GetPrereqs() EnginePrereqs {
	return EnginePrereqs{
		Session: true,
	}
}

func (k *PGPEngine) RequiredUIs() []libkb.UIKind {
	return []libkb.UIKind{
		libkb.LogUIKind,
		libkb.SecretUIKind,
	}
}

func (s *PGPEngine) SubConsumers() []libkb.UIConsumer {
	return nil
}

func (s *PGPEngine) init() (err error) {
	if s.arg.Gen != nil {
		err = s.arg.Gen.Init()
	}
	return err
}

func (s *PGPEngine) testExisting() (err error) {
	return PGPCheckMulti(s.me, s.arg.AllowMulti)

}

func (s *PGPEngine) Run(ctx *Context, args interface{}, reply interface{}) (err error) {
	G.Log.Debug("+ PGPEngine::Run")
	defer func() {
		G.Log.Debug("- PGPEngine::Run -> %s", libkb.ErrToOk(err))
	}()

	if err = s.init(); err != nil {
	} else if err = s.loadMe(); err != nil {
	} else if err = s.testExisting(); err != nil {
	} else if err = s.loadDelegator(ctx); err != nil {
	} else if err = s.generate(ctx); err != nil {
	} else if err = s.push(ctx); err != nil {
	} else if err = s.exportToGPG(ctx); err != nil {
	}

	return
}

func (s *PGPEngine) exportToGPG(ctx *Context) (err error) {
	if !s.arg.DoExport || s.arg.Pregen != nil {
		G.Log.Debug("| Skipping export to GPG")
		return
	}
	gpg := G.GetGpgClient()

	if _, err := gpg.Configure(); err != nil {
		if err == exec.ErrNotFound {
			G.Log.Debug("Not saving new key to GPG since no gpg install was found")
			err = nil
		}
		return err
	}
	err = gpg.ExportKey(*s.bundle)
	if err == nil {
		ctx.LogUI.Info("Exported new key to the local GPG keychain")
	}
	return err
}

func (s *PGPEngine) loadDelegator(ctx *Context) (err error) {

	s.del = &libkb.Delegator{
		ExistingKey: s.arg.SigningKey,
		Me:          s.me,
		Expire:      libkb.KEY_EXPIRE_IN,
		Sibkey:      true,
	}

	return s.del.LoadSigningKey(ctx.SecretUI)
}

func (s *PGPEngine) generate(ctx *Context) (err error) {

	G.Log.Debug("+ PGP::Generate")
	defer func() {
		G.Log.Debug("- PGP::Generate -> %s", libkb.ErrToOk(err))
	}()

	G.Log.Debug("| GenerateKey")
	if s.arg.Pregen != nil {
		s.bundle = s.arg.Pregen
	} else if s.arg.Gen == nil {
		err = libkb.InternalError{"PGPEngine: need either Gen or Pregen"}
		return
	} else if err = s.generateKey(ctx); err != nil {
		return
	}

	G.Log.Debug("| WriteKey")
	if s.arg.NoSave {
	} else if err = s.saveLKS(ctx); err != nil {
		return
	}

	if !s.arg.PushSecret {
	} else if err = s.prepareSecretPush(ctx); err != nil {
		return
	}
	return

}

func (s *PGPEngine) prepareSecretPush(ctx *Context) (err error) {
	var tsec *triplesec.Cipher
	var skb *libkb.SKB
	if tsec, err = G.LoginState.GetVerifiedTriplesec(ctx.SecretUI); err != nil {
	} else if skb, err = s.bundle.ToSKB(tsec); err != nil {
	} else {
		s.epk, err = skb.ArmoredEncode()
	}
	return
}

func (s *PGPEngine) push(ctx *Context) (err error) {
	G.Log.Debug("+ PGP::Push")
	s.del.NewKey = s.bundle
	s.del.EncodedPrivateKey = s.epk
	if err = s.del.Run(); err != nil {
		return err
	}
	G.Log.Debug("- PGP::Push -> %s", libkb.ErrToOk(err))

	ctx.LogUI.Info("Generated and pushed new PGP key:")
	d := s.bundle.VerboseDescription()
	for _, line := range strings.Split(d, "\n") {
		ctx.LogUI.Info("  %s", line)
	}

	return nil
}

func PGPCheckMulti(me *libkb.User, allowMulti bool) (err error) {
	if allowMulti {
		return
	}
	if pgps := me.GetActivePgpKeys(false); len(pgps) > 0 {
		err = libkb.KeyExistsError{pgps[0].GetFingerprintP()}
	}
	return
}
