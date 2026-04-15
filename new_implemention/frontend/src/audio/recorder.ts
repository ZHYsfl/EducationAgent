/**
 * Browser-based audio recorder.
 *
 * Captures microphone input via getUserMedia with echoCancellation and
 * noiseSuppression, and produces base64-encoded PCM chunks suitable for
 * the backend VAD APIs.
 */

export interface RecorderConfig {
  sampleRate?: number
}

export interface RecordedSegment {
  base64: string
  durationMs: number
}

export class AudioRecorder {
  private stream: MediaStream | null = null
  private audioContext: AudioContext | null = null
  private source: MediaStreamAudioSourceNode | null = null
  private processor: ScriptProcessorNode | null = null
  private audioBuffer: Float32Array[] = []
  private sampleRate = 16000
  private startedAt = 0
  private isRecordingFlag = false

  constructor(config?: RecorderConfig) {
    this.sampleRate = config?.sampleRate ?? 16000
  }

  /**
   * Request microphone permission and start capturing audio.
   */
  async start(): Promise<void> {
    this.stream = await navigator.mediaDevices.getUserMedia({
      audio: {
        echoCancellation: true,
        noiseSuppression: true,
        autoGainControl: true,
        sampleRate: this.sampleRate,
        channelCount: 1,
      },
    })

    this.audioContext = new AudioContext({ sampleRate: this.sampleRate })
    await this.audioContext.resume()

    this.source = this.audioContext.createMediaStreamSource(this.stream)
    this.processor = this.audioContext.createScriptProcessor(4096, 1, 1)

    this.audioBuffer = []
    this.startedAt = performance.now()
    this.isRecordingFlag = true

    this.processor.onaudioprocess = (event) => {
      const data = event.inputBuffer.getChannelData(0)
      // Copy because the underlying buffer may be reused.
      this.audioBuffer.push(new Float32Array(data))
    }

    this.source.connect(this.processor)
    this.processor.connect(this.audioContext.destination)
  }

  /**
   * Stop capturing and return the full recorded segment.
   */
  stop(): RecordedSegment | null {
    if (this.processor) {
      this.processor.onaudioprocess = null
      this.processor.disconnect()
      this.processor = null
    }
    if (this.source) {
      this.source.disconnect()
      this.source = null
    }
    if (this.audioContext) {
      this.audioContext.close()
      this.audioContext = null
    }
    this.stream?.getTracks().forEach((t) => t.stop())
    this.stream = null

    const segment = this.getFullSegment()
    this.isRecordingFlag = false
    this.audioBuffer = []
    return segment
  }

  /**
   * Extract a segment of recorded audio by relative time offsets.
   * Returns null if the recorder is not active or the offsets are invalid.
   */
  extractSegment(startMs: number, endMs: number): RecordedSegment | null {
    if (!this.isRecordingFlag || this.audioBuffer.length === 0) {
      return null
    }

    const sampleDuration = 1000 / this.sampleRate
    const startSample = Math.max(0, Math.floor(startMs / sampleDuration))
    const endSample = Math.max(startSample, Math.floor(endMs / sampleDuration))

    const totalLength = this.audioBuffer.reduce((sum, buf) => sum + buf.length, 0)
    if (endSample > totalLength) {
      return null
    }

    const segmentLength = endSample - startSample
    const segment = new Float32Array(segmentLength)

    let copied = 0
    let sampleOffset = 0
    for (const buf of this.audioBuffer) {
      const bufStart = sampleOffset
      const bufEnd = sampleOffset + buf.length

      if (bufEnd > startSample && bufStart < endSample) {
        const srcStart = Math.max(0, startSample - bufStart)
        const srcEnd = Math.min(buf.length, endSample - bufStart)
        const destStart = Math.max(0, bufStart - startSample)
        const slice = buf.subarray(srcStart, srcEnd)
        segment.set(slice, copied + destStart)
        copied += slice.length
      }
      sampleOffset += buf.length
      if (sampleOffset >= endSample) break
    }

    const pcm16 = floatTo16BitPCM(segment)
    const base64 = arrayBufferToBase64(pcm16.buffer)
    return { base64, durationMs: endMs - startMs }
  }

  /**
   * Get the full recording from start to now.
   */
  getFullSegment(): RecordedSegment | null {
    if (!this.isRecordingFlag) return null
    const elapsed = performance.now() - this.startedAt
    return this.extractSegment(0, elapsed)
  }

  /**
   * Return the elapsed recording time in milliseconds.
   */
  getElapsedMs(): number {
    if (!this.isRecordingFlag) return 0
    return performance.now() - this.startedAt
  }

  isRecording(): boolean {
    return this.isRecordingFlag
  }
}

function floatTo16BitPCM(input: Float32Array): DataView {
  const buffer = new ArrayBuffer(input.length * 2)
  const view = new DataView(buffer)
  for (let i = 0; i < input.length; i++) {
    const s = Math.max(-1, Math.min(1, input[i]))
    view.setInt16(i * 2, s < 0 ? s * 0x8000 : s * 0x7fff, true) // little-endian
  }
  return view
}

function arrayBufferToBase64(buffer: ArrayBufferLike): string {
  const bytes = new Uint8Array(buffer)
  let binary = ''
  for (let i = 0; i < bytes.byteLength; i++) {
    binary += String.fromCharCode(bytes[i])
  }
  return btoa(binary)
}
