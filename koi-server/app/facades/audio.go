package facades

import (
	contractsaudio "koi-server/app/contracts/audio"
)

// Audio 返回音频实时转写服务实例。
func Audio() contractsaudio.Transcriber {
	instance, err := App().Make(contractsaudio.Binding)
	if err != nil {
		panic("Failed to make audio transcriber: " + err.Error())
	}
	return instance.(contractsaudio.Transcriber)
}
