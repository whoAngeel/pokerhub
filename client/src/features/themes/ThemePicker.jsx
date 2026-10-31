import React from 'react';
import { THEMES } from './themes';

export default function ThemePicker({ value, onChange }) {
  return (
    <label className="theme-picker">
      <span>{THEMES[value].icon}</span>
      <select
        aria-label="Temática del casino"
        value={value}
        onChange={(event) => onChange(event.target.value)}
      >
        {Object.entries(THEMES).map(([id, theme]) => (
          <option key={id} value={id}>
            {theme.label}
          </option>
        ))}
      </select>
    </label>
  );
}
