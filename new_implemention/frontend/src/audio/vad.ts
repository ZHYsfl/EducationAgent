/**
 * Simple energy-based Voice Activity Detection (VAD) using the Web Audio API.
 *
 * The detector runs continuously while the microphone is active and emits
 * `onSpeechStart` / `onSpeechEnd` callbacks based on RMS energy thresholds.
 */

export interface VADConfig {
  /** Threshold in dB above which audio is considered speech. */
  thresholdDb: number
  /** Minimum duration of speech before `onSpeechStart` fires (ms). */
  minSpeechDurationMs: number
  /** Minimum duration of silence before `onSpeechEnd` fires (ms). */
  minSilenceDurationMs: number
  /** FFT size for the analyser node. */
  fftSize?: number
}

export interface VADCallbacks {
  onSpeechStart: () => void
  onSpeechEnd: () => void
  onError?: (err: Error) => void
}

export class VADetector {
  private stream: MediaStream | null = null
  private audioContext: AudioContext | null = null
  private analyser: AnalyserNode | null = null
  private source: MediaStreamAudioSourceNode | null = null
  private rafId: number | null = null

  private config: VADConfig
  private callbacks: VADCallbacks

  private isSpeaking = false
  private speechStartAt = 0
  private silenceStartAt = 0

  constructor(config: VADConfig, callbacks: VADCallbacks) {
    this.config = {
      fftSize: 2048,
      ...config,
    }
    this.callbacks = callbacks
  }

  /**
   * Start the VAD detector.
   */
  async start(): Promise<void> {
    this.stream = await navigator.mediaDevices.getUserMedia({
      audio: {
        echoCancellation: true,
        noiseSuppression: true,
        autoGainControl: true,
        channelCount: 1,
      },
    })

    this.audioContext = new AudioContext()
    await this.audioContext.resume()

    this.analyser = this.audioContext.createAnalyser()
    this.analyser.fftSize = this.config.fftSize ?? 2048

    this.source = this.audioContext.createMediaStreamSource(this.stream)
    this.source.connect(this.analyser)

    this.isSpeaking = false
    this.speechStartAt = 0
    this.silenceStartAt = performance.now()

    this.loop()
  }

  /**
   * Stop the VAD detector and release microphone resources.
   */
  stop() {
    if (this.rafId !== null) {
      cancelAnimationFrame(this.rafId)
      this.rafId = null
    }
    this.source?.disconnect()
    this.analyser = null
    this.audioContext?.close()
    this.audioContext = null
    this.stream?.getTracks().forEach((t) => t.stop())
    this.stream = null
  }

  private loop = () => {
    if (!this.analyser) return
    const dataArray = new Uint8Array(this.analyser.fftSize)
    this.analyser.getByteTimeDomainData(dataArray)

    const rms = computeRMS(dataArray)
    const db = 20 * Math.log10(rms + 1e-10)
    const now = performance.now()

    if (db > this.config.thresholdDb) {
      if (!this.isSpeaking) {
        if (this.speechStartAt === 0) {
          this.speechStartAt = now
        }
        if (now - this.speechStartAt >= this.config.minSpeechDurationMs) {
          this.isSpeaking = true
          this.silenceStartAt = 0
          this.callbacks.onSpeechStart()
        }
      } else {
        this.silenceStartAt = 0
      }
    } else {
      if (this.isSpeaking) {
        if (this.silenceStartAt === 0) {
          this.silenceStartAt = now
        }
        if (now - this.silenceStartAt >= this.config.minSilenceDurationMs) {
          this.isSpeaking = false
          this.speechStartAt = 0
          this.callbacks.onSpeechEnd()
        }
      } else {
        this.speechStartAt = 0
      }
    }

    this.rafId = requestAnimationFrame(this.loop)
  }
}

function computeRMS(data: Uint8Array): number {
  let sum = 0
  for (let i = 0; i < data.length; i++) {
    const normalized = (data[i] - 128) / 128.0
    sum += normalized * normalized
  }
  return Math.sqrt(sum / data.length)
}
