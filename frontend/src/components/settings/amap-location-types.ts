export interface AMapLngLat {
  getLng(): number;
  getLat(): number;
}

export interface AMapMap {
  add(overlay: AMapMarker): void;
  destroy(): void;
  off(event: "click", handler: (event: { lnglat: AMapLngLat }) => void): void;
  on(event: "click", handler: (event: { lnglat: AMapLngLat }) => void): void;
  remove(overlay: AMapMarker): void;
  setCenter(position: [number, number]): void;
  setZoom(zoom: number): void;
}

export interface AMapMarker {
  setPosition(position: [number, number]): void;
}

export interface AutocompleteTip {
  id?: string;
  name?: string;
  district?: string;
  adcode?: string;
  location?: unknown;
}

export interface AutocompleteResult {
  tips?: AutocompleteTip[];
}

export interface GeocoderResult {
  info?: string;
  regeocode?: {
    formattedAddress?: string;
    addressComponent?: {
      province?: string;
      city?: string | string[];
      district?: string;
      adcode?: string;
    };
    pois?: Array<{ id?: string; name?: string }>;
  };
}

export interface GeolocationResult {
  position?: unknown;
}

export interface AMapAutocomplete {
  search(
    keyword: string,
    callback: (status: string, result: AutocompleteResult | string) => void,
  ): void;
}

export interface AMapGeocoder {
  getAddress(
    position: [number, number],
    callback: (status: string, result: GeocoderResult) => void,
  ): void;
}

export interface AMapGeolocation {
  getCurrentPosition(callback: (status: string, result: GeolocationResult | string) => void): void;
}

export interface AMapNamespace {
  Map: new (
    container: HTMLElement,
    options: { center: [number, number]; resizeEnable: boolean; zoom: number },
  ) => AMapMap;
  Marker: new (options: { position: [number, number] }) => AMapMarker;
  AutoComplete: new (options: { city: string }) => AMapAutocomplete;
  Geocoder: new (options: { city: string }) => AMapGeocoder;
  Geolocation: new (options: {
    convert: boolean;
    enableHighAccuracy: boolean;
    timeout: number;
  }) => AMapGeolocation;
}
