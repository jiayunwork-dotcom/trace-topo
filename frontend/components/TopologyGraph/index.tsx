'use client';

import { useEffect, useRef, useState } from 'react';
import * as d3 from 'd3';
import type { TopologyGraph as TopologyGraphType, TopologyNode, TopologyEdge } from '@/types';
import { statusColorMap } from '@/types';
import { formatNumber, formatDuration, formatPercent } from '@/lib/utils';

interface TopologyGraphProps {
  data: TopologyGraphType;
  onNodeClick?: (node: TopologyNode) => void;
  onEdgeClick?: (edge: TopologyEdge) => void;
}

interface D3Node extends d3.SimulationNodeDatum {
  id: string;
  name: string;
  qps: number;
  status: string;
  is_active: boolean;
}

interface D3Link extends d3.SimulationLinkDatum<D3Node> {
  source: string | D3Node;
  target: string | D3Node;
  call_count: number;
  avg_latency: number;
  p99_latency: number;
  error_rate: number;
  status: string;
  is_active: boolean;
}

export default function TopologyGraph({ data, onNodeClick, onEdgeClick }: TopologyGraphProps) {
  const svgRef = useRef<SVGSVGElement>(null);
  const containerRef = useRef<HTMLDivElement>(null);
  const [tooltip, setTooltip] = useState<{
    show: boolean;
    x: number;
    y: number;
    content: React.ReactNode;
  }>({ show: false, x: 0, y: 0, content: null });

  useEffect(() => {
    if (!svgRef.current || !containerRef.current || !data.nodes.length) return;

    const width = containerRef.current.clientWidth;
    const height = 600;

    const svg = d3.select(svgRef.current);
    svg.selectAll('*').remove();

    svg.attr('width', width).attr('height', height);

    const g = svg.append('g');

    const zoom = d3.zoom<SVGSVGElement, unknown>()
      .scaleExtent([0.3, 3])
      .on('zoom', (event) => {
        g.attr('transform', event.transform);
      });

    svg.call(zoom);

    const nodes: D3Node[] = data.nodes.map((n) => ({
      ...n,
    }));

    const links: D3Link[] = data.edges.map((e) => ({
      ...e,
    }));

    const maxQPS = Math.max(...nodes.map((n) => n.qps), 1);
    const maxCalls = Math.max(...links.map((l) => l.call_count), 1);

    const nodeSize = d3.scaleSqrt()
      .domain([0, maxQPS])
      .range([20, 60]);

    const linkWidth = d3.scaleSqrt()
      .domain([0, maxCalls])
      .range([1, 8]);

    const simulation = d3.forceSimulation<D3Node>(nodes)
      .force('link', d3.forceLink<D3Node>(links).id((d) => d.id).distance(150))
      .force('charge', d3.forceManyBody().strength(-500))
      .force('center', d3.forceCenter(width / 2, height / 2))
      .force('collision', d3.forceCollide().radius((d) => nodeSize(d.qps) + 10));

    const link = g.append('g')
      .selectAll('line')
      .data(links)
      .join('line')
      .attr('class', 'topology-link cursor-pointer')
      .attr('stroke', (d) => statusColorMap[d.status])
      .attr('stroke-opacity', (d) => (d.is_active ? 0.6 : 0.3))
      .attr('stroke-width', (d) => linkWidth(d.call_count))
      .on('mouseenter', (event, d) => {
        setTooltip({
          show: true,
          x: event.pageX,
          y: event.pageY,
          content: (
            <div className="text-sm">
              <div className="font-semibold">{typeof d.source === 'object' ? d.source.name : d.source} → {typeof d.target === 'object' ? d.target.name : d.target}</div>
              <div>调用次数: {formatNumber(d.call_count)}</div>
              <div>平均延迟: {formatDuration(d.avg_latency)}</div>
              <div>P99延迟: {formatDuration(d.p99_latency)}</div>
              <div>错误率: {formatPercent(d.error_rate)}</div>
            </div>
          ),
        });
      })
      .on('mousemove', (event) => {
        setTooltip((prev) => ({ ...prev, x: event.pageX, y: event.pageY }));
      })
      .on('mouseleave', () => {
        setTooltip((prev) => ({ ...prev, show: false }));
      })
      .on('click', (event, d) => {
        if (onEdgeClick) {
          onEdgeClick(d as unknown as TopologyEdge);
        }
      });

    const node = g.append('g')
      .selectAll('g')
      .data(nodes)
      .join('g')
      .attr('class', 'topology-node')
      .call(d3.drag<D3DragEvent, D3Node, D3Node>()
        .on('start', (event, d) => {
          if (!event.active) simulation.alphaTarget(0.3).restart();
          d.fx = d.x;
          d.fy = d.y;
        })
        .on('drag', (event, d) => {
          d.fx = event.x;
          d.fy = event.y;
        })
        .on('end', (event, d) => {
          if (!event.active) simulation.alphaTarget(0);
          d.fx = null;
          d.fy = null;
        }));

    node.append('circle')
      .attr('r', (d) => nodeSize(d.qps))
      .attr('fill', (d) => statusColorMap[d.status])
      .attr('fill-opacity', (d) => (d.is_active ? 0.8 : 0.4))
      .attr('stroke', '#fff')
      .attr('stroke-width', 2)
      .on('mouseenter', (event, d) => {
        setTooltip({
          show: true,
          x: event.pageX,
          y: event.pageY,
          content: (
            <div className="text-sm">
              <div className="font-semibold">{d.name}</div>
              <div>QPS: {formatNumber(d.qps)}</div>
              <div>状态: {d.status}</div>
            </div>
          ),
        });
      })
      .on('mousemove', (event) => {
        setTooltip((prev) => ({ ...prev, x: event.pageX, y: event.pageY }));
      })
      .on('mouseleave', () => {
        setTooltip((prev) => ({ ...prev, show: false }));
      })
      .on('click', (event, d) => {
        if (onNodeClick) {
          onNodeClick(d as unknown as TopologyNode);
        }
      });

    node.append('text')
      .text((d) => d.name)
      .attr('text-anchor', 'middle')
      .attr('dy', (d) => nodeSize(d.qps) + 15)
      .attr('font-size', '12px')
      .attr('fill', '#374151')
      .style('pointer-events', 'none');

    simulation.on('tick', () => {
      link
        .attr('x1', (d) => (d.source as D3Node).x!)
        .attr('y1', (d) => (d.source as D3Node).y!)
        .attr('x2', (d) => (d.target as D3Node).x!)
        .attr('y2', (d) => (d.target as D3Node).y!);

      node.attr('transform', (d) => `translate(${d.x},${d.y})`);
    });

    return () => {
      simulation.stop();
    };
  }, [data, onNodeClick, onEdgeClick]);

  return (
    <div ref={containerRef} className="relative w-full">
      <svg ref={svgRef} className="w-full bg-gray-50 dark:bg-gray-800 rounded-lg" />
      {tooltip.show && (
        <div
          className="fixed z-50 pointer-events-none bg-white dark:bg-gray-800 border border-gray-200 dark:border-gray-700 rounded-lg shadow-lg p-3"
          style={{ left: tooltip.x + 10, top: tooltip.y + 10 }}
        >
          {tooltip.content}
        </div>
      )}
    </div>
  );
}
