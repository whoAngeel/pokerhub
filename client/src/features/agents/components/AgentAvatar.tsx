import type { Agent } from '../../casino/model/casino';

interface AgentAvatarProps {
  agent: Agent;
  size?: number;
}

export default function AgentAvatar({ agent, size = 42 }: AgentAvatarProps) {
  return (
    <svg
      width={size}
      height={size}
      viewBox="0 0 24 28"
      shapeRendering="crispEdges"
      aria-hidden="true"
    >
      <path fill="#13121c" d="M6 2h12v3h3v12h-3v3h3v8H3v-8h3v-3H3V5h3z" />
      <path fill={agent.color} d="M6 3h12v3h3v5H3V6h3zM6 20h12v8H6z" />
      <path fill="#efc8a0" d="M6 10h12v8H6zM9 18h6v3H9z" />
      <path fill="#29212b" d="M7 11h3v3H7zM14 11h3v3h-3zM10 16h4v1h-4z" />
      <path fill="#fff" opacity=".5" d="M7 4h3v3H7zM8 22h2v3H8z" />
      {agent.kind % 2 === 0 && <path fill={agent.color} d="M3 0h15v3H3zM0 3h21v3H0z" />}
    </svg>
  );
}
