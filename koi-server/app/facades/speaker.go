package facades

import (
	contractsspeaker "koi-server/app/contracts/speaker"
)

// Speaker 返回说话人声纹服务实例。
func Speaker() contractsspeaker.Voiceprint {
	instance, err := App().Make(contractsspeaker.Binding)
	if err != nil {
		panic("Failed to make speaker voiceprint: " + err.Error())
	}
	return instance.(contractsspeaker.Voiceprint)
}
