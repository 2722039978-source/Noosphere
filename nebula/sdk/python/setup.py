from setuptools import setup, find_packages

setup(
    name="nebula-agent",
    version="0.1.0",
    description="Nebula Agent — Lightweight Embedded Memory Engine for AI Agents",
    author="Nebula Agent Team",
    packages=find_packages(),
    install_requires=[
        "requests>=2.28",
    ],
    python_requires=">=3.9",
    classifiers=[
        "Development Status :: 3 - Alpha",
        "Intended Audience :: Developers",
        "License :: OSI Approved :: MIT License",
        "Programming Language :: Python :: 3",
    ],
)
