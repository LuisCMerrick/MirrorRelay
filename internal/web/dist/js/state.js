// Shared mutable UI state. Properties are mutated in place so every
// module that imports this object observes the same values.
export const state = {
  csrf: '',
  role: '',
  mirrors: [],
  profiles: [],
  customConfigs: [],
  signedIn: false,
  currentPage: 'dashboard',
};
