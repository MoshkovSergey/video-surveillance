export interface DiscoveredDevice {
  host: string;
  port: number;
  name: string;
  manufacturer: string;
  model: string;
  hardware: string;
  xaddrs: string[];
  added: boolean;
}

export interface ScanResult {
  devices: DiscoveredDevice[];
  duration_ms: number;
}