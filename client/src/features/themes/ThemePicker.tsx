import { THEMES, type ThemeId } from './themes';

interface ThemePickerProps {
  value: ThemeId;
  onChange: (value: ThemeId) => void;
}

export default function ThemePicker({ value, onChange }: ThemePickerProps) {
  return (
    <label className="theme-picker">
      <span>{THEMES[value].icon}</span>
      <select
        aria-label="Temática del casino"
        value={value}
        onChange={(event) => onChange(event.target.value as ThemeId)}
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
