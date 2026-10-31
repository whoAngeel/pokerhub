import { useEffect, useRef, type ReactNode } from 'react';
import { X } from 'lucide-react';

interface DialogProps {
  title: ReactNode;
  onClose: () => void;
  children: ReactNode;
}

export default function Dialog({ title, onClose, children }: DialogProps) {
  const ref = useRef<HTMLDialogElement>(null);
  useEffect(() => {
    const dialog = ref.current;
    if (!dialog) return;
    const previousFocus = document.activeElement as HTMLElement | null;
    dialog.showModal();
    return () => {
      dialog.close();
      previousFocus?.focus();
    };
  }, []);
  return (
    <dialog
      ref={ref}
      className="dialog"
      aria-labelledby="dialog-title"
      onCancel={onClose}
      onClick={(event) => {
        if (event.target === event.currentTarget) {
          const rect = event.currentTarget.getBoundingClientRect();
          if (
            event.clientX < rect.left ||
            event.clientX > rect.right ||
            event.clientY < rect.top ||
            event.clientY > rect.bottom
          )
            onClose();
        }
      }}
    >
      <button className="close-button" onClick={onClose} aria-label="Cerrar">
        <X size={20} />
      </button>
      <h2 id="dialog-title">{title}</h2>
      {children}
    </dialog>
  );
}
