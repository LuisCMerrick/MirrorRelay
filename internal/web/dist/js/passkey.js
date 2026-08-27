// WebAuthn / Passkey helper functions for browser credential creation and assertion.
import { api } from './api.js';
import { L } from './i18n.js';

export function bufferToBase64URL(buffer) {
  const bytes = new Uint8Array(buffer);
  let binary = '';
  for (let i = 0; i < bytes.byteLength; i++) {
    binary += String.fromCharCode(bytes[i]);
  }
  return btoa(binary)
    .replace(/\+/g, '-')
    .replace(/\//g, '_')
    .replace(/=+$/, '');
}

export function base64URLToBuffer(base64url) {
  let base64 = (base64url || '').replace(/-/g, '+').replace(/_/g, '/');
  while (base64.length % 4) {
    base64 += '=';
  }
  const binary = atob(base64);
  const bytes = new Uint8Array(binary.length);
  for (let i = 0; i < binary.length; i++) {
    bytes[i] = binary.charCodeAt(i);
  }
  return bytes.buffer;
}

export function isPasskeySupported() {
  return Boolean(window.PublicKeyCredential && navigator.credentials?.create && navigator.credentials?.get);
}

export async function loginWithPasskey(username) {
  if (!isPasskeySupported()) {
    throw new Error(L('WebAuthn / Passkey is not supported in this browser.'));
  }
  const options = await api('/auth/passkey/login/options', {
    method: 'POST',
    body: JSON.stringify({ username: username || '' })
  });

  const getOptions = {
    challenge: base64URLToBuffer(options.challenge),
    rpId: options.rpId,
    timeout: options.timeout || 60000,
    userVerification: options.userVerification || 'required'
  };

  if (options.allowCredentials && options.allowCredentials.length > 0) {
    getOptions.allowCredentials = options.allowCredentials.map(cred => ({
      type: cred.type,
      id: base64URLToBuffer(cred.id),
      transports: cred.transports
    }));
  }

  const credential = await navigator.credentials.get({ publicKey: getOptions });
  if (!credential) {
    throw new Error(L('No passkey credential returned.'));
  }

  const payload = {
    id: credential.id,
    rawId: bufferToBase64URL(credential.rawId),
    type: credential.type,
    response: {
      clientDataJSON: bufferToBase64URL(credential.response.clientDataJSON),
      authenticatorData: bufferToBase64URL(credential.response.authenticatorData),
      signature: bufferToBase64URL(credential.response.signature),
      userHandle: credential.response.userHandle ? bufferToBase64URL(credential.response.userHandle) : ''
    }
  };

  return await api('/auth/passkey/login/verify', {
    method: 'POST',
    body: JSON.stringify(payload)
  });
}

export async function registerPasskey(displayName) {
  if (!isPasskeySupported()) {
    throw new Error(L('WebAuthn / Passkey is not supported in this browser.'));
  }
  const options = await api('/account/passkeys/register/options', {
    method: 'POST'
  });

  const createOptions = {
    challenge: base64URLToBuffer(options.challenge),
    rp: options.rp,
    user: {
      id: base64URLToBuffer(options.user.id),
      name: options.user.name,
      displayName: options.user.displayName
    },
    pubKeyCredParams: options.pubKeyCredParams,
    timeout: options.timeout || 60000,
    attestation: options.attestation || 'none',
    authenticatorSelection: options.authenticatorSelection
  };

  if (options.excludeCredentials && options.excludeCredentials.length > 0) {
    createOptions.excludeCredentials = options.excludeCredentials.map(cred => ({
      type: cred.type,
      id: base64URLToBuffer(cred.id),
      transports: cred.transports
    }));
  }

  const credential = await navigator.credentials.create({ publicKey: createOptions });
  if (!credential) {
    throw new Error(L('Passkey registration cancelled.'));
  }

  const transports = credential.response.getTransports ? credential.response.getTransports() : [];

  const payload = {
    display_name: displayName || '',
    id: credential.id,
    rawId: bufferToBase64URL(credential.rawId),
    type: credential.type,
    transports: transports,
    response: {
      clientDataJSON: bufferToBase64URL(credential.response.clientDataJSON),
      attestationObject: bufferToBase64URL(credential.response.attestationObject)
    }
  };

  return await api('/account/passkeys/register/verify', {
    method: 'POST',
    body: JSON.stringify(payload)
  });
}
