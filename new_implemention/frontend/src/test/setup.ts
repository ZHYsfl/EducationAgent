import '@testing-library/jest-dom'
import { vi } from 'vitest'

// Polyfill for MediaRecorder and Web Audio API
global.navigator.mediaDevices = {
  getUserMedia: vi.fn(),
} as unknown as MediaDevices

global.MediaRecorder = vi.fn(() => ({
  start: vi.fn(),
  stop: vi.fn(),
  pause: vi.fn(),
  resume: vi.fn(),
  ondataavailable: null,
  onstop: null,
  onerror: null,
  state: 'inactive',
})) as unknown as typeof MediaRecorder

// Polyfill AudioContext (must use a regular function so `new AudioContext()` works)
global.AudioContext = vi.fn(function () {
  return {
    createMediaStreamSource: vi.fn(() => ({
      connect: vi.fn(),
      disconnect: vi.fn(),
    })),
    createAnalyser: vi.fn(() => ({
      connect: vi.fn(),
      disconnect: vi.fn(),
      fftSize: 2048,
      getByteTimeDomainData: vi.fn(),
      getByteFrequencyData: vi.fn(),
    })),
    createScriptProcessor: vi.fn(() => ({
      connect: vi.fn(),
      disconnect: vi.fn(),
      onaudioprocess: null,
    })),
    destination: {},
    state: 'running',
    resume: vi.fn(() => Promise.resolve()),
    suspend: vi.fn(),
    close: vi.fn(),
  }
}) as unknown as typeof AudioContext
