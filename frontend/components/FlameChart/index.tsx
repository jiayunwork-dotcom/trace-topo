'use client';

import { useEffect, useRef, useState } from 'react';
import * as d3 from 'd3';
import type { SpanTreeNode } from '@/types';
import { formatDuration, generateColor } from '@/lib/utils';

interface FlameChartProps {
  spans: SpanTreeNode[];
  onSpanClick?: (span: SpanTreeNode) => void;
}

interface LayoutSpan {
  span: SpanTreeNode;
  depth: number;
  start: number;
  duration: number;
  color: string;
}

export default function FlameChart({ spans, onSpanClick }: FlameChartProps) {
  const svgRef = useRef<SVGSVGElement>(null);
  const containerRef = useRef<HTMLDivElement>(null);
  const [tooltip, setTooltip] = useState<{
    show: boolean;
    x: number;
    y: number;
    content: React.ReactNode;
  }>({ show: false, x: 0, y: 0, content: null });

  useEffect(() => {
    if (!svgRef.current || !containerRef.current || !spans.length) return;

    const width = containerRef.current.clientWidth;
    const rowHeight = 35;

    const flatSpans: LayoutSpan[] = [];
    const serviceColors: Record<string, string> = {};

    let colorIndex = 0;
    const getColor = (service: string) => {
      if (!serviceColors[service]) {
        serviceColors[service] = generateColor(colorIndex++);
      }
      return serviceColors[service];
    };

    const startTime = Math.min(...spans.map((s) => s.start_time));
    const maxDuration = Math.max(...spans.map((s) => s.start_time + s.duration_ms - startTime));

    const flatten = (node: SpanTreeNode, depth: number) => {
      flatSpans.push({
        span: node,
        depth,
        start: node.start_time - startTime,
        duration: node.duration_ms,
        color: getColor(node.service_name),
      });
      node.children?.forEach((child) => flatten(child, depth + 1));
    };

    const roots = spans.filter((s) => !s.parent_span_id);
    roots.forEach((root) => flatten(root, 0));

    const maxDepth = Math.max(...flatSpans.map((s) => s.depth), 0);
    const height = (maxDepth + 2) * rowHeight;

    const svg = d3.select(svgRef.current);
    svg.selectAll('*').remove();

    svg.attr('width', width).attr('height', height);

    const xScale = d3.scaleLinear()
      .domain([0, maxDuration])
      .range([50, width - 20]);

    const g = svg.append('g');

    const xAxis = d3.axisBottom(xScale)
      .ticks(10)
      .tickFormat((d) => `${d}ms`);

    g.append('g')
      .attr('transform', `translate(0,${height - 20})`)
      .call(xAxis)
      .selectAll('text')
      .attr('fill', '#6b7280');

    g.selectAll('.domain, .tick line')
      .attr('stroke', '#e5e7eb');

    const bars = g.selectAll('g.flame-bar')
      .data(flatSpans)
      .join('g')
      .attr('class', 'flame-bar cursor-pointer')
      .attr('transform', (d) => `translate(${xScale(d.start)}, ${d.depth * rowHeight + 10})`);

    bars.append('rect')
      .attr('width', (d) => Math.max(xScale(d.duration) - xScale(0) - 2, 2))
      .attr('height', rowHeight - 6)
      .attr('rx', 3)
      .attr('fill', (d) => d.color)
      .attr('fill-opacity', 0.85)
      .attr('stroke', '#fff')
      .attr('stroke-width', 1)
      .on('mouseenter', (event, d) => {
        setTooltip({
          show: true,
          x: event.pageX,
          y: event.pageY,
          content: (
            <div className="text-sm">
              <div className="font-semibold">{d.span.service_name}: {d.span.operation_name}</div>
              <div>耗时: {formatDuration(d.duration)}</div>
              <div>状态: {d.span.status_code === 2 ? '错误' : d.span.status_code === 1 ? 'OK' : '未设置'}</div>
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
        if (onSpanClick) {
          onSpanClick(d.span);
        }
      });

    bars.append('text')
      .text((d) => `${d.span.service_name}: ${d.span.operation_name}`)
      .attr('x', 5)
      .attr('y', rowHeight / 2 - 2)
      .attr('font-size', '11px')
      .attr('fill', '#fff')
      .attr('text-anchor', 'start')
      .style('pointer-events', 'none')
      .each(function(d) {
        const self = d3.select(this);
        const textLength = self.node()!.getComputedTextLength();
        const barWidth = Math.max(xScale(d.duration) - xScale(0) - 12, 0);
        if (textLength > barWidth) {
          self.text(d.span.service_name + ': ' + d.span.operation_name);
          let truncated = d.span.service_name + ': ' + d.span.operation_name;
          while (self.node()!.getComputedTextLength() > barWidth && truncated.length > 5) {
            truncated = truncated.slice(0, -1);
            self.text(truncated + '...');
          }
        }
      });

    bars.append('text')
      .text((d) => formatDuration(d.duration))
      .attr('x', (d) => Math.max(xScale(d.duration) - xScale(0) - 5, 15))
      .attr('y', rowHeight / 2 - 2)
      .attr('font-size', '10px')
      .attr('fill', '#fff')
      .attr('text-anchor', 'end')
      .style('pointer-events', 'none');

  }, [spans, onSpanClick]);

  return (
    <div ref={containerRef} className="relative w-full overflow-x-auto">
      <svg ref={svgRef} className="w-full bg-white dark:bg-gray-900 rounded-lg" />
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
